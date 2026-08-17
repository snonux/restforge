// Integration test: drives the app's SERVICE layer -- session.dart, composing
// nav_service.dart/action_service.dart/live_service.dart/http_service.dart --
// against the real fixture Siren API (pebble/tools/fake-siren-server.py) over
// real HTTP on localhost. No mocks, no device, no emulator.
//
// *** How the fixture server is run, and why ***
// This runs under plain `flutter test`, the same `just -f flutter/Justfile
// test` path every other test in this suite uses: `setUpAll` spawns
// `python3 pebble/tools/fake-siren-server.py 8731` as a subprocess and waits
// for its own startup banner on stderr before running anything -- a
// poll-and-read loop, not a fixed sleep, which is exactly the kind of
// startup race a fixed sleep would leave flaky. `tearDownAll` sends SIGTERM
// and waits for the process to exit, falling back to SIGKILL if it does
// not. A separate Justfile recipe was the documented fallback if the
// subprocess-in-test approach proved awkward (a Python startup race, port
// contention with something else in the suite); neither problem showed up
// here -- nothing else under test/ opens a real socket, every other service
// test drives http_service.dart through package:http's MockClient -- so the
// flutter-test-native path was kept. This is pure Dart-VM-level I/O
// (dart:io's Process/Stream), which flutter test runs without any device or
// emulator, exactly like every other test here.
//
// This drives session.dart's actual public API -- never a mock -- so what it
// proves is the wiring end to end, not any single service's own logic
// already pinned in that service's unit test (nav_service_test.dart,
// action_service_test.dart, live_service_test.dart, http_service_test.dart,
// session_test.dart -- all against a fake in-process route table, never a
// real socket). In particular this is the first test in this repository to
// exercise pebble/docs/DESIGN.md's "never carry a document across an
// action" against a *real* HTTP server: browsing from the root, opening a
// property, following a link, invoking an action, confirming it, and
// watching the unconditional re-fetch afterward actually change what is on
// screen -- sourced from a fresh GET, never from the action's own response.
//
// It also drives every awkward case the fixture exists to reproduce, each as
// an assertion rather than something dodged: a 401 (wrong secret), a 409
// (two confirmed brews racing each other), a non-JSON response (/notjson), a
// request that outruns the read timeout (/slow, against a shortened timeout
// budget so the test does not wait out the real 20s production default), a
// job that runs for a genuine 24 seconds server-side (/job, watched through
// LiveService, polled faster than the 10s production default so the wait is
// bounded rather than doubled -- the server's own clock is never faked,
// since job_state() in fake-siren-server.py derives progress from
// time.time()), and a required field with no default that only a human can
// fill (label-jar's "text" field).
//
// Deliberately one test, not several: the fixture server is one Python
// process holding mutable state (a kettle, a brew count, a running job) with
// no reset endpoint, so the awkward cases below are woven into a single
// ordered scenario rather than independent test() cases that would either
// race each other or have to agree on execution order by accident.

import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:restforge/models/failure.dart';
import 'package:restforge/services/http_service.dart';
import 'package:restforge/services/live_service.dart';
import 'package:restforge/services/nav_service.dart';
import 'package:restforge/services/render_service.dart';
import 'package:restforge/services/session.dart';
import 'package:restforge/services/settings_service.dart';

const int _fixturePort = 8731;
const String _fixtureSecret = 'open-sesame';
const String _fixtureBaseUrl = 'http://127.0.0.1:$_fixturePort/';

/// Walks up from the current working directory looking for
/// `pebble/tools/fake-siren-server.py`. `flutter test` (and so
/// `just -f flutter/Justfile test`) runs with the working directory set to
/// `flutter/`, one level below the repo root that also holds `pebble/`, but
/// this climbs a few levels rather than hardcoding that assumption, so the
/// test does not silently start failing the moment something about how it
/// is invoked changes.
String _locateFixtureScript() {
  var dir = Directory.current;
  for (var i = 0; i < 5; i++) {
    final candidate = File('${dir.path}/pebble/tools/fake-siren-server.py');
    if (candidate.existsSync()) {
      return candidate.path;
    }
    final parent = dir.parent;
    if (parent.path == dir.path) {
      break;
    }
    dir = parent;
  }
  throw StateError(
    'could not find pebble/tools/fake-siren-server.py above '
    '${Directory.current.path} -- run tests from flutter/ '
    '(just -f flutter/Justfile test)',
  );
}

/// Finds the first row matching [match], or fails with a readable dump of
/// what was actually on the document -- a bare `firstWhere` throwing
/// `StateError: No element` gives no clue which of several fetches produced
/// the document being searched.
Row _rowWhere(RenderedDocument doc, bool Function(Row) match, String what) {
  for (final row in doc.rows) {
    if (match(row)) {
      return row;
    }
  }
  throw StateError(
    'no row for $what among: '
    '${doc.rows.map((r) => '${r.kind.name}:${r.label}').join(', ')}',
  );
}

/// Polls [condition] on a real wall-clock timer until it is true or
/// [timeout] elapses -- used to wait out the fixture's genuine 24-second
/// job without a fixed sleep tied to a guess about exactly how long that
/// takes plus network round trips.
Future<void> _waitUntil(
  bool Function() condition, {
  required Duration timeout,
  Duration step = const Duration(milliseconds: 400),
}) async {
  final deadline = DateTime.now().add(timeout);
  while (!condition() && DateTime.now().isBefore(deadline)) {
    await Future<void>.delayed(step);
  }
}

void main() {
  late Process fixtureProcess;
  late StreamSubscription<String> fixtureStderr;

  setUpAll(() async {
    final script = _locateFixtureScript();
    fixtureProcess = await Process.start('python3', [
      script,
      '$_fixturePort',
    ]);
    // Drained for the whole process lifetime, not just until "ready": the
    // fixture logs one line per request (see its log_message override), and
    // an unread stderr pipe can eventually make a child process block on
    // write once enough accumulates -- this test sends several dozen
    // requests over its run.
    fixtureProcess.stdout.listen((_) {});
    final stderrLines = <String>[];
    var ready = false;
    fixtureStderr = fixtureProcess.stderr
        .transform(utf8.decoder)
        .transform(const LineSplitter())
        .listen((line) {
          stderrLines.add(line);
          if (line.contains('fixture Siren server on')) {
            ready = true;
          }
        });

    final deadline = DateTime.now().add(const Duration(seconds: 10));
    while (!ready && DateTime.now().isBefore(deadline)) {
      await Future<void>.delayed(const Duration(milliseconds: 100));
    }
    if (!ready) {
      throw StateError(
        'fixture server did not report ready in time; stderr so far:\n'
        '${stderrLines.join('\n')}',
      );
    }
  });

  tearDownAll(() async {
    fixtureProcess.kill(ProcessSignal.sigterm);
    await fixtureProcess.exitCode.timeout(
      const Duration(seconds: 5),
      onTimeout: () {
        fixtureProcess.kill(ProcessSignal.sigkill);
        return -1;
      },
    );
    await fixtureStderr.cancel();
  });

  test(
    'browse, open a property, follow a link, invoke, confirm, and observe '
    'the unconditional re-fetch -- plus every awkward case the fixture '
    'reproduces on demand',
    () async {
      final http = HttpService(
        // Real localhost round trips finish in single-digit milliseconds;
        // shortening both budgets keeps the one deliberately slow case
        // below (/slow) from waiting out the 20s production default, while
        // leaving every other request all the headroom it could plausibly
        // need.
        getTimeoutOverride: const Duration(seconds: 4),
        actionTimeoutOverride: const Duration(seconds: 8),
      );
      final backend = Backend(
        name: 'fixture',
        baseUrl: _fixtureBaseUrl,
        secret: _fixtureSecret,
      );

      // === Awkward case: a 401 for a wrong key ==========================
      // A separate, throwaway session against the same server with the
      // wrong secret, so the very first fetch is the one that fails -- run
      // before the main session opens so nothing about it depends on state
      // the happy path below goes on to create.
      final wrongSecret = SessionService(http: http);
      await wrongSecret.openBackend(
        Backend(
          name: 'fixture-wrong-key',
          baseUrl: _fixtureBaseUrl,
          secret: 'not-the-right-key',
        ),
      );
      expect(wrongSecret.state, DocumentState.error);
      expect(wrongSecret.failure?.kind, FailureKind.auth);
      expect(wrongSecret.failure?.status, 401);
      // The server's own wording, not this app's invention -- see
      // http_service.dart's _describe/_serverMessage.
      expect(wrongSecret.failure?.message, 'API key rejected');
      expect(wrongSecret.document, isNull);
      wrongSecret.dispose();

      // The session the rest of this test drives. LiveService gets its own
      // instance with a shortened poll interval -- 2s instead of the
      // production 10s -- so waiting out the fixture's genuine 24-second job
      // below has a bounded, reasonable cost without faking the server's
      // own clock: job_state() in fake-siren-server.py derives progress
      // from time.time(), which this test cannot and should not fake.
      final live = LiveService(
        http: http,
        createTimer: (duration, callback) =>
            Timer(const Duration(seconds: 2), callback),
      );
      final session = SessionService(http: http, live: live);

      try {
        // === Browse from the root ======================================
        await session.openBackend(backend);
        expect(session.state, DocumentState.ok);
        var doc = session.document!;
        expect(doc.title, 'The pantry');

        final kettleRow = _rowWhere(
          doc,
          (r) => r.kind == RowKind.property && r.label == 'kettle',
          'kettle property',
        );
        expect(kettleRow.sublabel, 'cold');

        final rootActionLabels = doc.rows
            .where((r) => r.kind == RowKind.action)
            .map((r) => r.label)
            .toSet();
        // brew and label-jar are always offered; cool-down only once the
        // kettle is hot -- not yet.
        expect(rootActionLabels, contains('Brew a pot of tea'));
        expect(rootActionLabels, contains('Write a label for a jar'));
        expect(rootActionLabels, isNot(contains('Let the kettle cool')));

        // === Open a property ============================================
        await session.activate(kettleRow.target);
        expect(session.detail?.heading, 'kettle');
        expect(session.detail?.body, 'cold');
        session.dismissDetail();
        expect(session.detail, isNull);

        // === Follow a link ===============================================
        final shelvesRow = _rowWhere(
          doc,
          (r) => r.kind == RowKind.link && r.label == 'shelves',
          'shelves link',
        );
        await session.activate(shelvesRow.target);
        expect(session.state, DocumentState.ok);
        expect(session.document!.title, 'Shelves');
        expect(session.canGoBack, isTrue);
        session.back();
        expect(session.canGoBack, isFalse);
        expect(session.document!.title, 'The pantry');

        // === Awkward case: a required field only a human can fill ========
        // label-jar's "text" field has no default and nothing (a checkbox
        // the user just ticked) to stand in for it -- fillFields() must ask
        // out loud, never invent one.
        doc = session.document!;
        final labelRow = _rowWhere(
          doc,
          (r) => r.kind == RowKind.action && r.label == 'Write a label for a jar',
          'label-jar action',
        );
        await session.activate(labelRow.target);
        expect(session.question, isA<ConfirmQuestion>());
        await session.answer(true);
        expect(session.question, isA<ValueQuestion>());
        expect(
          (session.question as ValueQuestion).label,
          'What should the label say?',
        );
        await session.answerValue('Earl Grey');
        expect(session.notice, isA<ActionOutcomeReported>());
        // A 200 with no "state" property is not something to watch, and the
        // response itself is never shown as the document -- session.state
        // reflects the re-fetch that followed, not the POST's own reply.
        expect(session.isLive, isFalse);
        expect(session.state, DocumentState.ok);

        // === Awkward case: a response that is not JSON at all ============
        await session.activate(const FetchTarget('/notjson'));
        expect(session.state, DocumentState.error);
        expect(session.failure?.kind, FailureKind.parse);
        expect(session.failure?.status, 200);
        expect(session.failure?.message, 'response was not JSON');
        // A failed request is not an answer -- the pantry document stays.
        expect(session.document?.title, 'The pantry');

        // Note: the read-timeout case (/slow) is deliberately exercised last,
        // not here -- see the comment above it, near the end of this test.

        // === Invoke an action, confirm it =================================
        doc = session.document!;
        final brewRow = _rowWhere(
          doc,
          (r) => r.kind == RowKind.action && r.label == 'Brew a pot of tea',
          'brew action',
        );
        await session.activate(brewRow.target);
        expect(session.question, isA<ConfirmQuestion>());
        final confirm = session.question as ConfirmQuestion;
        expect(confirm.heading, 'Brew a pot of tea');
        expect(confirm.body, 'Brew a pot of tea?  POST to this server.');
        await session.answer(true);

        // The server answered 202 with a still-running job -- session.dart
        // hands it to LiveService rather than reporting a 202 as done, so
        // the notice says "running" and the document on screen is still the
        // stale, pre-action one: nothing has re-fetched yet.
        expect(session.notice, isA<ActionOutcomeReported>());
        expect((session.notice as ActionOutcomeReported).message, 'running');
        expect(session.isLive, isTrue);
        expect(
          session.document!.rows
              .firstWhere((r) => r.kind == RowKind.property && r.label == 'kettle')
              .sublabel,
          'cold',
        );

        // === Awkward case: a 409 -- two confirmed brews racing ============
        // The document on screen is still the stale one fetched before the
        // first brew, so "brew" is still there to look up and re-invoke --
        // exactly the race pebble/docs/DESIGN.md's "never carry a document
        // across an action" exists to make safe: it is the server, not this
        // client, that notices the state changed underneath it.
        await session.activate(brewRow.target);
        expect(session.question, isA<ConfirmQuestion>());
        await session.answer(true);
        expect(session.notice, isA<ActionFailed>());
        final conflict = (session.notice as ActionFailed).failure;
        expect(conflict.kind, FailureKind.conflict);
        expect(conflict.status, 409);
        expect(conflict.message, 'a brew is already running');
        // A conflict is re-fetched, never retried -- the document now
        // reflects the first brew's real, already-applied effect, read back
        // from a fresh GET, not from either action's own POST response.
        expect(session.state, DocumentState.ok);
        expect(
          session.document!.rows
              .firstWhere((r) => r.kind == RowKind.property && r.label == 'kettle')
              .sublabel,
          'hot',
        );
        expect(
          session.document!.rows
              .firstWhere((r) => r.kind == RowKind.property && r.label == 'brews')
              .sublabel,
          '1',
        );
        // Still brewing -- cool-down is not offered again until it stops.
        expect(
          session.document!.rows
              .where((r) => r.kind == RowKind.action)
              .map((r) => r.label)
              .toSet(),
          isNot(contains('Let the kettle cool')),
        );
        // The watch on the first job is untouched by the second, failed one
        // -- only a *successful* action outcome ever restarts LiveService.
        expect(session.isLive, isTrue);

        // === Awkward case: a job that takes a genuine 24 seconds, and =====
        // === the unconditional re-fetch once it ends =======================
        var sawProgress = false;
        await _waitUntil(() {
          if (session.notice is ActionProgress) {
            sawProgress = true;
          }
          return !session.isLive;
        }, timeout: const Duration(seconds: 40));

        expect(
          session.isLive,
          isFalse,
          reason: 'the 24s job should have finished within the wait budget',
        );
        expect(
          sawProgress,
          isTrue,
          reason: 'expected at least one progress step while watching',
        );
        expect(session.notice, isA<ActionOutcomeReported>());
        expect((session.notice as ActionOutcomeReported).message, 'done');
        // The document shown is a fresh GET, never the job's own response (a
        // "kettle-job" entity, a different shape entirely) -- the pantry is
        // still what is rendered, now reflecting what the finished job
        // actually did.
        expect(session.document!.title, 'The pantry');
        expect(
          session.document!.rows
              .firstWhere((r) => r.kind == RowKind.property && r.label == 'brews')
              .sublabel,
          '1',
        );
        expect(
          session.document!.rows
              .firstWhere((r) => r.kind == RowKind.property && r.label == 'kettle')
              .sublabel,
          'hot',
        );
        // Only visible now that the job is done and the kettle is hot --
        // information none of the earlier, stale documents could have had.
        expect(
          session.document!.rows
              .where((r) => r.kind == RowKind.action)
              .map((r) => r.label)
              .toSet(),
          contains('Let the kettle cool'),
        );

        // === Awkward case: a request that outruns the read timeout ========
        // Exercised last, deliberately: fake-siren-server.py is a plain
        // (single-threaded, non-forking) http.server.HTTPServer, so /slow's
        // time.sleep(30) blocks the *entire* server, not just this one
        // connection -- giving up locally after 4s does not free it up
        // again for a follow-up request, which would otherwise queue behind
        // that sleep and time out itself for a reason that has nothing to
        // do with what it is testing. Nothing follows this in the test, so
        // that does not matter here.
        await session.activate(const FetchTarget('/slow'));
        expect(session.state, DocumentState.unreachable);
        expect(session.failure?.kind, FailureKind.timeout);
        expect(session.failure?.message, 'timed out');
        expect(session.document?.title, 'The pantry');
      } finally {
        session.dispose();
      }
    },
    timeout: const Timeout(Duration(minutes: 2)),
  );
}
