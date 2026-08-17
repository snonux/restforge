/// RESTForge for Android: a generic Siren hypermedia browser.
///
/// This is the phone-native sibling of the Pebble watchapp in ../pebble. The
/// watchapp splits itself across two devices because it has to — the watch has
/// no IP stack — and the phone half (PebbleKit JS) ends up owning every HTTP
/// request, every Siren document, the navigation stack and all the secrets.
///
/// Here there is only one device, so that split does not exist. What survives
/// the port is the part that was never about the hardware:
///
///   Fetch the root. Render what it offers. Never build a URL.
///
/// A client that obeys that cannot contain server-specific code, which is what
/// makes it reusable against an API it has never seen. See ../README.md for
/// how the two apps relate, and AGENTS.md here for the invariants a change to
/// this one must not break.
library;

import 'package:flutter/material.dart';

import 'screens/home_screen.dart';

void main() {
  runApp(const RestForgeApp());
}

class RestForgeApp extends StatelessWidget {
  const RestForgeApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'RESTForge',
      theme: ThemeData(
        colorScheme: ColorScheme.fromSeed(seedColor: Colors.deepOrange),
        useMaterial3: true,
      ),
      darkTheme: ThemeData(
        colorScheme: ColorScheme.fromSeed(
          seedColor: Colors.deepOrange,
          brightness: Brightness.dark,
        ),
        useMaterial3: true,
      ),
      home: const HomeScreen(),
    );
  }
}
