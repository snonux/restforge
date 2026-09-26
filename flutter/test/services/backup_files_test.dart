// Pure Dart unit tests for the file-dialog side of the settings backup. The
// platform dialog itself is replaced by the constructor's test seams; what is
// pinned here is what PickerBackupFiles does with the result — above all that
// on Android it deletes file_picker's cache copy of the picked file, which
// would otherwise leave every secret in plain text in the app's cache.

import 'dart:convert';
import 'dart:typed_data';

import 'package:file_picker/file_picker.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:restforge/models/result.dart';
import 'package:restforge/services/backup_files.dart';
import 'package:restforge/services/backup_service.dart';

FilePickerResult _picked(List<int> bytes) => FilePickerResult([
  PlatformFile(
    name: 'backup.json',
    size: bytes.length,
    bytes: Uint8List.fromList(bytes),
  ),
]);

void main() {
  late int clears;

  setUp(() => clears = 0);

  PickerBackupFiles files(
    Future<FilePickerResult?> Function() pick, {
    bool android = true,
    Future<bool?> Function()? clear,
  }) => PickerBackupFiles(
    clearsPickerCopies: android,
    pick: pick,
    clearPickerCopies:
        clear ??
        () async {
          clears++;
          return true;
        },
  );

  test('reads the picked file and then clears the cache copy', () async {
    final result = await files(
      () async => _picked(utf8.encode('{"a": "ü"}')),
    ).open();
    expect(result, isA<Ok<String?>>());
    expect((result as Ok<String?>).value, '{"a": "ü"}');
    expect(clears, 1);
  });

  test('a cancelled pick still clears', () async {
    final result = await files(() async => null).open();
    expect((result as Ok<String?>).value, isNull);
    expect(clears, 1);
  });

  test('a file that is not UTF-8 is refused, and still cleared', () async {
    final result = await files(() async => _picked([0xff, 0xfe, 0x00])).open();
    expect((result as Err<String?>).failure.message, contains('not text'));
    expect(clears, 1);
  });

  test('an oversized file is refused, and still cleared', () async {
    final result = await files(
      () async => FilePickerResult([
        PlatformFile(
          name: 'big.json',
          size: BackupService.maxFileBytes + 1,
          bytes: Uint8List(0),
        ),
      ]),
    ).open();
    expect((result as Err<String?>).failure.message, contains('too large'));
    expect(clears, 1);
  });

  test('a failing picker is reported, and still cleared', () async {
    final result = await files(
      () async => throw Exception('no activity'),
    ).open();
    expect((result as Err<String?>).failure.message, contains('no activity'));
    expect(clears, 1);
  });

  test('a failure to clear does not fail the import', () async {
    final result = await files(
      () async => _picked(utf8.encode('{}')),
      clear: () async => throw Exception('busy'),
    ).open();
    expect((result as Ok<String?>).value, '{}');
  });

  test('off Android there is no copy to clear', () async {
    await files(() async => _picked(utf8.encode('{}')), android: false).open();
    expect(clears, 0);
  });
}
