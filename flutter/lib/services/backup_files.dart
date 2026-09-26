/// Where a settings backup is written to and read from: a file the user
/// picks, through the platform's own file dialog.
///
/// On Android that is the Storage Access Framework (create-document to save,
/// open-document to load), so the app needs no storage permission and the
/// user can put the file anywhere a document provider reaches — Downloads, a
/// USB stick, a cloud drive's app. On Linux desktop, the development target,
/// it is the desktop's file chooser. Both come from `file_picker`.
///
/// The contract is an interface so the home screen's widget tests can swap
/// in an in-memory fake: the real plugin talks to a platform channel a
/// `flutter test` process does not have — the same reason [SecretStore]
/// exists.
library;

import 'dart:convert';
import 'dart:io';

import 'package:file_picker/file_picker.dart';
import 'package:flutter/foundation.dart';

import '../models/failure.dart';
import '../models/result.dart';
import 'backup_service.dart';

abstract class BackupFiles {
  /// Asks the user where to save [contents] as [fileName] and writes it
  /// there. [Ok] carries where it went — or null when the user cancelled.
  Future<Result<String?>> save(String fileName, String contents);

  /// Asks the user for a backup file and returns its text. [Ok] carries
  /// null when the user cancelled.
  Future<Result<String?>> open();
}

/// [BackupFiles] through the platform file dialog — see the module comment.
class PickerBackupFiles implements BackupFiles {
  PickerBackupFiles({
    bool? clearsPickerCopies,
    @visibleForTesting Future<FilePickerResult?> Function()? pick,
    @visibleForTesting Future<bool?> Function()? clearPickerCopies,
  }) : _clearsPickerCopies = clearsPickerCopies ?? Platform.isAndroid,
       _pick = pick ?? _pickWithPlatformDialog,
       _clearPickerCopies = clearPickerCopies ?? FilePicker.clearTemporaryFiles;

  /// Whether [open] has to delete the plugin's copy of the picked file. On
  /// Android file_picker copies whatever was picked into
  /// `cacheDir/file_picker/<timestamp>/` — even with `withData` — and never
  /// removes it, which for a backup would leave every secret in plain text
  /// in the app's cache. Elsewhere it reads the original in place and has
  /// nothing to clear (and `clearTemporaryFiles` is unimplemented on Linux).
  /// Saving writes straight to the chosen document and leaves no copy.
  final bool _clearsPickerCopies;
  final Future<FilePickerResult?> Function() _pick;
  final Future<bool?> Function() _clearPickerCopies;

  // FileType.any rather than a .json filter: several Android document
  // providers do not map .json to a MIME type and would grey the file out.
  // A wrong file is refused by BackupService.parse, with a reason.
  static Future<FilePickerResult?> _pickWithPlatformDialog() =>
      FilePicker.pickFiles(dialogTitle: 'Import settings', withData: true);

  @override
  Future<Result<String?>> save(String fileName, String contents) async {
    try {
      final location = await FilePicker.saveFile(
        dialogTitle: 'Export settings',
        fileName: fileName,
        type: FileType.custom,
        allowedExtensions: const ['json'],
        // Written by the plugin itself: on Android the chosen location is a
        // content URI this process cannot open as a path.
        bytes: Uint8List.fromList(utf8.encode(contents)),
      );
      return Ok(location);
    } catch (error) {
      return Err(
        Failure(
          kind: FailureKind.config,
          message: 'Could not save the file: $error',
        ),
      );
    }
  }

  @override
  Future<Result<String?>> open() async {
    try {
      final picked = await _pick();
      if (picked == null || picked.files.isEmpty) {
        return const Ok(null);
      }
      final file = picked.files.single;
      if (file.size > BackupService.maxFileBytes) {
        return const Err(
          Failure(
            kind: FailureKind.parse,
            message:
                'This file is too large to be a RESTForge settings backup.',
          ),
        );
      }
      final bytes = file.bytes ?? await File(file.path!).readAsBytes();
      return Ok(utf8.decode(bytes));
    } on FormatException {
      return const Err(
        Failure(
          kind: FailureKind.parse,
          message: 'This is not a RESTForge settings backup: it is not text.',
        ),
      );
    } catch (error) {
      return Err(
        Failure(
          kind: FailureKind.config,
          message: 'Could not read the file: $error',
        ),
      );
    } finally {
      await _deletePickerCopies();
    }
  }

  /// See [_clearsPickerCopies]. Runs after every pick, read or not, and
  /// removes the plugin's whole cache folder, so a copy left behind by an
  /// earlier pick that never got this far goes too. A failure to clear is
  /// logged, not reported: the import itself is unaffected.
  Future<void> _deletePickerCopies() async {
    if (!_clearsPickerCopies) {
      return;
    }
    try {
      if (await _clearPickerCopies() != true) {
        debugPrint('backup: the picked file\'s cache copy was not cleared');
      }
    } catch (error) {
      debugPrint(
        'backup: could not clear the picked file\'s cache copy: $error',
      );
    }
  }
}
