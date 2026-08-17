/* RFC 3986 reference resolution.
 *
 * ES5 ONLY; see the note at the top of appmessage.js.
 *
 * This exists because PebbleKit JS has no URL constructor -- verified in the
 * emulator, and the device runtime is older still.  So the one line the Node
 * reference client spends on this,
 *
 *     new URL(href, BASE).toString()
 *
 * has to be written out.  It is section 5.2 of RFC 3986, no more and no less.
 *
 * Note what this is *not*: it is not path construction.  The path always comes
 * from the server, in an href it put in a document; only the scheme and
 * authority are ours, because a server behind a reverse proxy cannot know the
 * public name the client reached it by.  That distinction is the whole of the
 * hypermedia rule -- follow what you were given, never assemble a path from
 * knowledge you think you have about the server.
 */

'use strict';

/* RFC 3986 appendix B.  Components that did not appear come back undefined,
 * which the algorithm below distinguishes from an empty one: "?" with nothing
 * after it is an empty query, and it is not the same as no query at all. */
var URI_RE = /^(?:([^:\/?#]+):)?(?:\/\/([^\/?#]*))?([^?#]*)(?:\?([^#]*))?(?:#(.*))?$/;

function parse(uri) {
  var match = URI_RE.exec(uri || '');
  return {
    scheme: match[1],
    authority: match[2],
    path: match[3] || '',
    query: match[4],
    fragment: match[5]
  };
}

/* removeDotSegments is RFC 3986 section 5.2.4, written as the specification
 * writes it: consume the input buffer from the left, push and pop an output
 * stack.  A shorter split/join version gets the trailing-slash cases wrong. */
function removeDotSegments(path) {
  var input = path;
  var output = [];

  while (input.length) {
    if (input.indexOf('../') === 0) {
      input = input.slice(3);
    } else if (input.indexOf('./') === 0) {
      input = input.slice(2);
    } else if (input.indexOf('/./') === 0) {
      input = '/' + input.slice(3);
    } else if (input === '/.') {
      input = '/';
    } else if (input.indexOf('/../') === 0) {
      input = '/' + input.slice(4);
      output.pop();
    } else if (input === '/..') {
      input = '/';
      output.pop();
    } else if (input === '.' || input === '..') {
      input = '';
    } else {
      /* Move the first path segment, including its leading slash, across. */
      var next = input.indexOf('/', 1);
      var segment = next < 0 ? input : input.slice(0, next);
      output.push(segment);
      input = input.slice(segment.length);
    }
  }
  return output.join('');
}

/* merge is RFC 3986 section 5.3: a relative path is appended to the base's
 * path with the base's last segment removed.  This is why settings.js insists
 * on a trailing slash -- without one, "https://host/cgi-bin/app" + "thing"
 * resolves to "https://host/cgi-bin/thing", one level too high, and the only
 * symptom is a 404 two screens later. */
function merge(base, relativePath) {
  if (base.authority !== undefined && base.path === '') {
    return '/' + relativePath;
  }
  var cut = base.path.lastIndexOf('/');
  return cut < 0 ? relativePath : base.path.slice(0, cut + 1) + relativePath;
}

function recompose(parts) {
  var out = '';
  if (parts.scheme !== undefined) {
    out += parts.scheme + ':';
  }
  if (parts.authority !== undefined) {
    out += '//' + parts.authority;
  }
  out += parts.path;
  if (parts.query !== undefined) {
    out += '?' + parts.query;
  }
  if (parts.fragment !== undefined) {
    out += '#' + parts.fragment;
  }
  return out;
}

/* target implements the five-way branch of RFC 3986 section 5.2.2. */
function target(reference, base) {
  if (reference.scheme !== undefined) {
    return { scheme: reference.scheme, authority: reference.authority,
             path: removeDotSegments(reference.path), query: reference.query };
  }
  if (reference.authority !== undefined) {
    return { scheme: base.scheme, authority: reference.authority,
             path: removeDotSegments(reference.path), query: reference.query };
  }
  if (reference.path === '') {
    return { scheme: base.scheme, authority: base.authority, path: base.path,
             query: reference.query !== undefined ? reference.query : base.query };
  }
  return {
    scheme: base.scheme,
    authority: base.authority,
    path: removeDotSegments(reference.path.charAt(0) === '/'
                            ? reference.path
                            : merge(base, reference.path)),
    query: reference.query
  };
}

/* resolve returns href resolved against base, both given as strings. */
function resolve(href, base) {
  var reference = parse(href);
  var parts = target(reference, parse(base));
  parts.fragment = reference.fragment;
  return recompose(parts);
}

/* origin is the scheme and authority of a URL, for logging a request without
 * repeating the whole thing on every line. */
function origin(url) {
  var parts = parse(url);
  if (parts.scheme === undefined || parts.authority === undefined) {
    return '';
  }
  return parts.scheme + '://' + parts.authority;
}

/* path is everything after the origin -- what goes in a log line, and the part
 * that is safe to log because it never carries the key. */
function path(url) {
  var parts = parse(url);
  return parts.path + (parts.query !== undefined ? '?' + parts.query : '');
}

/* encodeForm builds an application/x-www-form-urlencoded body.  Percent
 * encoding is encodeURIComponent's, with the space-as-plus convention that
 * form encoding requires and URI encoding does not. */
function encodeForm(fields) {
  var parts = [];
  for (var name in fields) {
    if (Object.prototype.hasOwnProperty.call(fields, name)) {
      parts.push(encodeURIComponent(name).replace(/%20/g, '+') + '=' +
                 encodeURIComponent(String(fields[name])).replace(/%20/g, '+'));
    }
  }
  return parts.join('&');
}

module.exports = {
  resolve: resolve,
  origin: origin,
  path: path,
  encodeForm: encodeForm
};
