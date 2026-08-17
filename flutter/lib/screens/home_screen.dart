/// The opening screen: the backend picker.
///
/// In the finished app this lists the configured backends and the saved
/// shortcuts, and picking one fetches its root document. Right now it is the
/// skeleton's only screen and says so — see the agent task list for the work
/// that replaces it.
library;

import 'package:flutter/material.dart';

class HomeScreen extends StatelessWidget {
  const HomeScreen({super.key});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Scaffold(
      appBar: AppBar(title: const Text('RESTForge')),
      body: Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(
            mainAxisAlignment: MainAxisAlignment.center,
            children: [
              Icon(
                Icons.travel_explore,
                size: 64,
                color: theme.colorScheme.primary,
              ),
              const SizedBox(height: 16),
              Text('No backends configured', style: theme.textTheme.titleMedium),
              const SizedBox(height: 8),
              Text(
                'A backend is an API root, an auth header and a secret. '
                'Add one and RESTForge will render whatever it offers.',
                textAlign: TextAlign.center,
                style: theme.textTheme.bodyMedium,
              ),
            ],
          ),
        ),
      ),
    );
  }
}
