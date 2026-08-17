/// HTTP for one configured backend.
///
/// This is the Dart port of `pebble/src/pkjs/http.js` — see that file's
/// header comment for the reasoning behind everything that carries over
/// unchanged:
///
/// - **Two timeout budgets, not one.** A read is quick or it is broken; an
///   action that changes physical state can legitimately take a long time,
///   so it gets its own, longer budget rather than making every request
///   wait a minute before giving up. [getTimeout] / [actionTimeout] are the
///   Dart names for `GET_TIMEOUT_MS` / `ACTION_TIMEOUT_MS`.
/// - **`FailureKind.unreachable` is distinguished from every other kind.**
///   A request that never arrived says nothing about the state of the
///   thing it asked about, and must never be rendered as if the server had
///   answered — see AGENTS.md section 5.
/// - **The secret goes in a request header, never a query string.** A key
///   in a URI is written to the server's access log and, behind a reverse
///   proxy, the proxy's log too.
/// - **Nothing in this file logs a header value we sent.** Response
///   headers are logged in full — which one identifies the answering node,
///   the cache or the proxy is a property of the deployment, not something
///   this generic client gets to assume — but a header this file *set*
///   (the auth header above all) never reaches `log`.
///
/// What did **not** carry over: the two XHR-specific workarounds `http.js`
/// exists to route around. PebbleKit JS's `XMLHttpRequest` fires neither
/// `onerror` nor a useful status on a DNS failure, TLS error or refused
/// connection — `status` just sits at 0, which is why `http.js` keys
/// unreachability off `readyState === 4 && status === 0` instead. And a
/// bodyless POST there omits `Content-Length`, which some servers reject
/// outright, hence `send('')` and never `send()`. `package:http` has
/// neither problem: a connection failure throws before any response
/// exists, and its request body is sent exactly as given. So there is no
/// status-0 branch here and no send-vs-send('') distinction — but the
/// *behaviour* those workarounds guaranteed is still a requirement, so the
/// tests that pin it (an unreachable host resolves as unreachable, not
/// silently; a field-less POST still carries a body) are ported in
/// `test/services/http_service_test.dart`.
library;

import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;

import '../models/failure.dart';
import '../models/result.dart';
import 'settings_service.dart';
import 'url_resolver.dart' as url;

/// The budget a `GET`/`HEAD` gets before it is reported as [FailureKind.timeout].
/// Carried over unchanged from `GET_TIMEOUT_MS` in http.js.
const Duration getTimeout = Duration(seconds: 20);

/// The budget anything else (an action) gets. Longer than [getTimeout]
/// because an action can legitimately take a long time to complete on the
/// server, and a client giving up early on a request that is still working
/// cannot tell that apart from one that failed. Carried over unchanged from
/// `ACTION_TIMEOUT_MS` in http.js.
const Duration actionTimeout = Duration(seconds: 60);

/// One successful exchange: the status, the URL it was actually fetched
/// from (after resolution against the backend's base), and the decoded
/// body.
///
/// [entity] is `dynamic`, not a Siren `Entity`, deliberately: this layer
/// knows only that a server speaks JSON, not what shape a particular
/// response is — that is `siren.dart`'s job, one layer up. Keeping this
/// type ignorant of Siren is what keeps `http_service.dart` free of any
/// server-specific vocabulary, same as `http.js` never importing `siren.js`.
@immutable
class HttpResponse {
  final int status;
  final String url;
  final dynamic entity;

  const HttpResponse({required this.status, required this.url, this.entity});
}

/// Performs one HTTP exchange against a configured [Backend] at a time.
///
/// The [http.Client] is injected so tests run against `package:http`'s
/// `MockClient` — no socket, no fixture server needed for the failure
/// cases that matter most (an unreachable host, a timeout). `log` is
/// injected for the same reason: it is `debugPrint` by default, and a
/// plain capturing function in tests, mirroring the way
/// `pebble/tools/test-http.js` monkey-patches `console.log` to assert what
/// did and did not reach it. `getTimeoutOverride`/`actionTimeoutOverride`
/// exist only for tests: production code always gets [getTimeout] and
/// [actionTimeout], but a test asserting the two budgets are actually
/// different has no reason to wait 20 real seconds to prove it.
class HttpService {
  HttpService({
    http.Client? client,
    void Function(String message)? log,
    Duration? getTimeoutOverride,
    Duration? actionTimeoutOverride,
  }) : _client = client ?? http.Client(),
       _log = log ?? debugPrint,
       _getTimeout = getTimeoutOverride ?? getTimeout,
       _actionTimeout = actionTimeoutOverride ?? actionTimeout;

  final http.Client _client;
  final void Function(String message) _log;
  final Duration _getTimeout;
  final Duration _actionTimeout;

  /// The common case, spelled out so callers do not pass three nulls —
  /// mirrors `get` in http.js.
  Future<Result<HttpResponse>> get(Backend backend, String href) =>
      request(backend, href, 'GET', null);

  /// Performs one HTTP exchange.
  ///
  /// [href] is whatever the server put in the document — absolute,
  /// root-relative or relative — and is resolved against [backend]'s base
  /// URL. Only the scheme and authority come from us; the path always came
  /// from the server (see `url_resolver.dart` and
  /// `pebble/docs/DESIGN.md`, "The rule everything else follows from").
  Future<Result<HttpResponse>> request(
    Backend backend,
    String href,
    String method,
    Map<String, Object?>? fields,
  ) async {
    final verb = method.toUpperCase();
    final target = url.resolve(href, backend.baseUrl);
    final isRead = verb == 'GET' || verb == 'HEAD';

    final headers = _buildHeaders(backend, hasBody: !isRead);
    if (headers == null) {
      return Err(
        Failure(
          kind: FailureKind.config,
          message: 'bad auth header name "${backend.authHeader}"',
        ),
      );
    }

    // send('') and never send(null) has no analogue here -- package:http
    // sends a request body exactly as given -- but the shape carries over:
    // anything that is not a read gets a body, even an empty one, so a
    // field-less action still declares a Content-Type.
    final body = isRead ? null : (fields == null ? '' : url.encodeForm(fields));
    final timeout = isRead ? _getTimeout : _actionTimeout;

    _log('$verb ${url.origin(target)}${url.path(target)}');
    final startedAt = DateTime.now();

    final http.Response response;
    try {
      response = await _exchange(verb, target, headers, body).timeout(timeout);
    } on TimeoutException {
      _log('$verb ${url.path(target)} -> timed out after ${timeout.inMilliseconds}ms');
      return Err(const Failure(kind: FailureKind.timeout, message: 'timed out'));
    } catch (error) {
      // Anything package:http/dart:io throws before a response exists --
      // DNS failure, TLS error, refused connection -- means the request
      // never arrived and the answer never will. That is exactly
      // FailureKind.unreachable: it says nothing about the state of the
      // thing we asked about, only that we could not ask.
      final host = url.origin(target);
      return Err(
        Failure(
          kind: FailureKind.unreachable,
          message: 'no answer from ${host.isEmpty ? target : host}',
        ),
      );
    }

    return _finish(verb, target, response, startedAt);
  }

  Future<http.Response> _exchange(
    String verb,
    String target,
    Map<String, String> headers,
    String? body,
  ) async {
    final request = http.Request(verb, Uri.parse(target))..headers.addAll(headers);
    if (body != null) {
      request.body = body;
    }
    final streamed = await _client.send(request);
    return http.Response.fromStream(streamed);
  }

  /// Turns a completed exchange into either an error or a result — mirrors
  /// `finish` in http.js, minus the `status === 0` branch: package:http
  /// never returns a response for a request that never got one, so that
  /// case is already handled by the `catch` in [request].
  Result<HttpResponse> _finish(
    String method,
    String target,
    http.Response response,
    DateTime startedAt,
  ) {
    _logResponse(method, target, response, startedAt);

    final entity = _parseBody(response.body);
    final kind = _classify(response.statusCode);
    if (kind != null) {
      return Err(
        Failure(
          kind: kind,
          status: response.statusCode,
          message: _describe(kind, response.statusCode, entity),
        ),
      );
    }
    if (entity == null) {
      return Err(
        Failure(
          kind: FailureKind.parse,
          status: response.statusCode,
          message: 'response was not JSON',
        ),
      );
    }
    return Ok(HttpResponse(status: response.statusCode, url: target, entity: entity));
  }

  /// Decodes [text] into the response body, or `null` when it was empty or
  /// not JSON. An error response is allowed to carry an explanation in the
  /// same shape, so this runs regardless of status — mirrors `parseBody`.
  dynamic _parseBody(String text) {
    if (text.isEmpty) {
      return null;
    }
    try {
      return jsonDecode(text);
    } on FormatException {
      return null;
    }
  }

  /// Maps an HTTP status onto a [FailureKind], or `null` for success. `202`
  /// is a success: it means the work was accepted and is still running,
  /// which is a normal answer, not a failure — mirrors `classify`.
  FailureKind? _classify(int status) {
    if (status >= 200 && status < 300) {
      return null;
    }
    if (status == 401 || status == 403) {
      return FailureKind.auth;
    }
    if (status == 409) {
      return FailureKind.conflict;
    }
    if (status >= 500) {
      return FailureKind.server;
    }
    return FailureKind.client;
  }

  /// Mirrors `describe`: the server's own wording beats anything this app
  /// could invent, because it is the only party that knows why it said no.
  String _describe(FailureKind kind, int status, dynamic entity) {
    final fromServer = _serverMessage(entity);
    if (fromServer != null) {
      return fromServer;
    }
    if (kind == FailureKind.auth) {
      return 'auth rejected';
    }
    if (kind == FailureKind.conflict) {
      return 'state changed';
    }
    return 'HTTP $status';
  }

  /// Digs the server's own wording out of an error envelope — mirrors
  /// `serverMessage`. Tolerant of anything that is not the expected shape,
  /// same as every other reader in this app: an error body that does not
  /// carry a message is not itself an error.
  String? _serverMessage(dynamic entity) {
    if (entity is Map) {
      final properties = entity['properties'];
      if (properties is Map) {
        final message = properties['message'];
        if (message is String) {
          return message;
        }
      }
    }
    return null;
  }

  /// Applies the auth header and the ones this app always sends. Mirrors
  /// `setHeaders`. A header name the user typed can be rejected by the
  /// platform's HTTP stack, so the name is checked against RFC 7230's
  /// token grammar here rather than letting whatever exception the
  /// underlying client throws be caught, generically, as
  /// [FailureKind.unreachable] — a bad header name is a local
  /// configuration problem, not a network one.
  Map<String, String>? _buildHeaders(Backend backend, {required bool hasBody}) {
    if (!_isValidHeaderName(backend.authHeader)) {
      return null;
    }
    final headers = <String, String>{
      backend.authHeader: backend.secret,
      'Accept': 'application/vnd.siren+json, application/json',
    };
    if (hasBody) {
      headers['Content-Type'] = 'application/x-www-form-urlencoded';
    }
    return headers;
  }

  static final RegExp _tokenPattern = RegExp(r"^[A-Za-z0-9!#$%&'*+.^_`|~-]+$");

  static bool _isValidHeaderName(String name) => _tokenPattern.hasMatch(name);

  /// Records what came back. Every response header is logged rather than a
  /// chosen few: which header identifies the answering node, the cache or
  /// the proxy is a property of the deployment, and naming one here would
  /// be knowledge of a particular server — mirrors `logResponse`. One log
  /// call per header rather than one with embedded newlines, same reason
  /// as the original: a multi-line message loses its timestamp and source
  /// on every line after the first.
  void _logResponse(String method, String target, http.Response response, DateTime startedAt) {
    final elapsed = DateTime.now().difference(startedAt).inMilliseconds;
    _log('$method ${url.path(target)} -> ${response.statusCode} (${elapsed}ms)');
    response.headers.forEach((name, value) {
      _log('  $name: $value');
    });
  }
}
