/// RFC 3986 reference resolution.
///
/// This is the Dart side of `pebble/src/pkjs/url.js`. That file exists only
/// because PebbleKit JS has no URL constructor, so RFC 3986 section 5.2 had
/// to be written out by hand. Dart's [Uri] already implements it, so
/// [resolve] delegates to [Uri.resolve] instead of reimplementing
/// `removeDotSegments`/`merge` -- every RFC 3986 section 5.4 test vector in
/// `test/services/url_resolver_test.dart` (ported line for line from
/// `pebble/tools/test-url.js`) passes against it unchanged, so there was no
/// case where Dart's algorithm needed correcting to match the specification.
///
/// Note what this is *not*: it is not path construction. The path always
/// comes from the server, in an href it put in a document; only the scheme
/// and authority are ours, because a server behind a reverse proxy cannot
/// know the public name the client reached it by. That distinction is the
/// whole of the hypermedia rule -- follow what you were given, never
/// assemble a path from knowledge you think you have about the server. See
/// pebble/docs/DESIGN.md, "The rule everything else follows from".
///
/// Every function here is total: given a string, it returns a string, never
/// throws. `url.js`'s hand-written parser is regex-based and accepts any
/// input, so it never fails either; [Uri.parse] is stricter about what an
/// RFC 3986 URI reference looks like (an unparsable port, an empty scheme
/// before a `:`, ...) and throws [FormatException] on the inputs it
/// rejects. Rather than pushing that exception onto every caller -- which
/// would need a `Result` for what is otherwise a pure function with no
/// caller-visible failure mode, see AGENTS.md section 5 -- each function
/// catches it and falls back to something safe to keep going with. A
/// request built from a fallback still goes out and fails naturally at the
/// transport layer (`http_service.dart`), the same place a request to a
/// merely-wrong-but-parsable URL would fail anyway.
library;

/// Resolve [href] against [base] and return the resulting absolute URL as a
/// string, per RFC 3986 section 5.2. Falls back to [href] unchanged if
/// either string is not a URI [Uri.parse] can make sense of.
String resolve(String href, String base) {
  try {
    return Uri.parse(base).resolve(href).toString();
  } on FormatException {
    return href;
  }
}

/// The scheme and authority of [url], e.g. `https://host.example.org` -- for
/// logging a request without repeating the whole thing on every line. Empty
/// if [url] has no scheme, no authority (a path-only, relative reference),
/// or is not parsable at all.
String origin(String url) {
  try {
    final parts = Uri.parse(url);
    if (parts.scheme.isEmpty || !parts.hasAuthority) {
      return '';
    }
    return '${parts.scheme}://${parts.authority}';
  } on FormatException {
    return '';
  }
}

/// Everything in [url] after the origin -- what goes in a log line, and the
/// part that is safe to log because it never carries the key. Falls back to
/// [url] unchanged if it is not parsable.
String path(String url) {
  try {
    final parts = Uri.parse(url);
    return parts.hasQuery ? '${parts.path}?${parts.query}' : parts.path;
  } on FormatException {
    return url;
  }
}

/// Build an `application/x-www-form-urlencoded` body from [fields].
///
/// Percent encoding is [Uri.encodeQueryComponent]'s, which already applies
/// the space-as-plus convention form encoding requires (and plain URI
/// encoding does not) -- matching `encodeForm` in `url.js` without that
/// file's hand-rolled `.replace(/%20/g, '+')`, which Dart's stdlib does not
/// need.
String encodeForm(Map<String, Object?> fields) {
  return fields.entries
      .map((entry) =>
          '${Uri.encodeQueryComponent(entry.key)}=${Uri.encodeQueryComponent(entry.value.toString())}')
      .join('&');
}
