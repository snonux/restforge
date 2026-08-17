import 'package:flutter_test/flutter_test.dart';
import 'package:restforge/main.dart';

void main() {
  testWidgets('the app starts on the backend picker', (tester) async {
    await tester.pumpWidget(const RestForgeApp());

    expect(find.text('RESTForge'), findsOneWidget);
    expect(find.text('No backends configured'), findsOneWidget);
  });
}
