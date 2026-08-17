// Integration test that captures README screenshots by driving the *real*
// app against the real fixture Siren API — `pebble/tools/fake-siren-server.py`,
// the one fixture that deliberately shares no vocabulary with any real
// server, so what these screenshots show is the one thing a single real
// backend cannot demonstrate: RESTForge rendering an API it has never seen.
//
// Unlike `test/integration/browse_confirm_act_refetch_test.dart` (which
// drives the *service* layer under plain `flutter test`, no device), this
// drives the *UI* — `RestForgeApp` root and all — on an Android emulator, so
// the screenshots are real device-class renders, the same way
// `pebble/screenshots/` holds the watchapp's. It is run on demand, not part
// of `just test` (which must stay device-free per AGENTS.md section 5); see
// the `screenshots` recipe in `Justfile` for the exact invocation.
//
// *** The fixture server is not started here. ***
// This test runs *on the device*, so a `dart:io` Process started from here
// would start on the device, not the host — useless. The fixture is started
// on the host beforehand (`just -f pebble/Justfile fixture`, port 8731) and
// reached from the emulator through `adb reverse tcp:8731 tcp:8731`, so the
// app's `http://127.0.0.1:8731/` resolves to the host's fixture exactly as
// `flutter/README.md`'s "physical device" note describes. If the fixture is
// not up, the very first fetch fails and the test fails loudly rather than
// producing a screenshot of an error screen.
//
// *** How the PNGs reach the host. ***
// `integration_test`'s `takeScreenshot` returns the PNG bytes in the test's
// own Dart isolate — which runs *on the device*. `flutter test` uninstalls
// the app (and its filesystem sandbox) the moment the test finishes, so
// writing the bytes to the device and `adb pull`-ing them afterwards finds
// nothing; the binding's `reportData`/`integration_response_data.json` path
// is the `flutter drive` workflow's, not `flutter test`'s. Instead each
// screenshot's bytes are base64-encoded and printed on a single tagged line
// — `RESTFORGE_SCREENSHOT\t<name>\t<base64>` — which the device's stdout
// forwards to the host's `flutter test` console; the `screenshots` Justfile
// recipe captures that console to a file and decodes the tagged lines into
// PNGs under `screenshots/`.

import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:restforge/main.dart' as app;
import 'package:restforge/services/quick_service.dart';
import 'package:restforge/services/settings_service.dart';

const String _fixtureBaseUrl = 'http://127.0.0.1:8731/';
const String _fixtureSecret = 'open-sesame';

/// Prints [bytes] as one tagged line the host-side recipe can grep out and
/// base64-decode. One line per screenshot so the parsing stays line-based
/// even if other test output is interleaved around it.
void _emitScreenshot(String name, List<int> bytes) {
  // ignore: avoid_print
  print('RESTFORGE_SCREENSHOT\t$name\t${base64Encode(bytes)}');
}

void main() {
  final binding = IntegrationTestWidgetsFlutterBinding.ensureInitialized();

  /// Pre-seeds a configured backend and one saved shortcut so the opening
  /// screen has something to show beyond the empty state — the same state a
  /// user reaches by adding a backend in the editor and long-pressing a row
  /// to save it. Done in `setUpAll` (before any `pumpWidget`) because the
  /// platform channels `shared_preferences`/`flutter_secure_storage` need
  /// are live as soon as the binding is initialised, and `HomeScreen` reads
  /// this storage in its `initState`, so it must be in place before the app
  /// first builds.
  setUpAll(() async {
    final settings = SettingsService();
    await settings.saveBackends([
      const Backend(
        name: 'Siren fixture',
        baseUrl: _fixtureBaseUrl,
        secret: _fixtureSecret,
      ),
    ]);
    final quick = QuickService(settings: settings);
    await quick.add(
      const QuickItem(
        label: 'The pantry',
        baseUrl: _fixtureBaseUrl,
        kind: QuickKind.document,
        href: '/',
      ),
    );
  });

  testWidgets(
    'capture README screenshots against the fixture',
    (tester) async {
      // Android renders the Flutter surface on a platform view until this is
      // called; without it, takeScreenshot captures a blank frame. Pump a
      // frame after it so the conversion takes effect before the first
      // capture.
      await binding.convertFlutterSurfaceToImage();
      await tester.pump();

      await tester.pumpWidget(const app.RestForgeApp());
      // The backend list and shortcuts both come from async storage, and
      // the FutureBuilder chain in HomeScreen needs a couple of pumps to
      // settle both futures.
      await tester.pumpAndSettle();

      // === 1. The opening screen: backends + a saved shortcut ============
      _emitScreenshot(
        'pantry_01_backends',
        await binding.takeScreenshot('pantry_01_backends'),
      );

      // === 2. Browsing: tap the backend, land on its root document =======
      // The pantry fixture renders every row kind — properties (kettle,
      // brews, apiVersion), an embedded sub-entity (a shelf), links
      // (shelves, kettle-job), and actions (brew, label-jar) — which is the
      // whole point of using it for the README: one screenshot shows the
      // app rendering a server it had never heard of until this fetch.
      //
      // The backend is tapped by its base-URL subtitle rather than its
      // name: the name also appears as the saved-shortcut tile's
      // resolved-backend subtitle, so `find.text('Siren fixture')` is
      // ambiguous.
      await tester.tap(
        find.ancestor(
          of: find.text('http://127.0.0.1:8731/'),
          matching: find.byType(ListTile),
        ),
      );
      await tester.pumpAndSettle();
      _emitScreenshot(
        'pantry_02_document',
        await binding.takeScreenshot('pantry_02_document'),
      );

      // === 3. Ask before acting: tap an unsafe action, see the confirm ===
      // sheet before anything is sent. pebble/docs/DESIGN.md's "the row
      // that opens a property and the row that changes the world must not
      // act the same" — the action row is a button-shaped tile, and
      // tapping it never fires the POST before this sheet is answered.
      await tester.tap(find.text('Brew a pot of tea'));
      await tester.pumpAndSettle();
      _emitScreenshot(
        'pantry_03_confirm',
        await binding.takeScreenshot('pantry_03_confirm'),
      );

      // === 4. Still running: confirm, get a 202, watch the job ===========
      // The fixture's brew returns a still-running job (24s server-side);
      // LiveService watches it rather than claiming it finished, and the
      // banner says "running" with a spinner. A short pump is enough — the
      // banner is set from the action's own 202 reply before the first
      // poll even lands.
      await tester.tap(find.text('Confirm'));
      // Not pumpAndSettle: the live poll Timer is periodic, so the frame
      // never "settles" while a watch is live. A fixed-duration pump lets
      // the POST round-trip and the banner render without waiting for a
      // settle that will not come.
      await tester.pump(const Duration(seconds: 2));
      _emitScreenshot(
        'pantry_04_running',
        await binding.takeScreenshot('pantry_04_running'),
      );

      // === 5. Done: the job finishes, the document is re-fetched =========
      // The unconditional re-fetch pebble/docs/DESIGN.md requires: the
      // pantry comes back from a fresh GET reflecting what the finished
      // job actually did (kettle now hot, cool-down offered), never from
      // the job's own response. Waiting real time (the binding is live, so
      // pump(duration) advances real time and lets the real poll Timer
      // fire) for the fixture's genuine 24s job plus a poll interval.
      await tester.pump(const Duration(seconds: 34));
      await tester.pump(const Duration(milliseconds: 500));
      _emitScreenshot(
        'pantry_05_done',
        await binding.takeScreenshot('pantry_05_done'),
      );
    },
    // The 24s job plus emulator round trips needs headroom past the default
    // 30s test timeout.
    timeout: const Timeout(Duration(minutes: 2)),
  );
}