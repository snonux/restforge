/* The settings page shown by the Pebble phone app.
 *
 * ES5 ONLY -- both this module and the page's own inline script.  The page runs
 * in a browser that would accept far more, but keeping one dialect across
 * src/pkjs means the ES5 grep in AGENTS.md stays a single check with no
 * exceptions to remember.
 *
 * Hand-written rather than built with Clay: Clay's schema is a fixed list of
 * slots decided at build time, and this page has to add and remove backends at
 * will.  That is the whole feature.
 *
 * The page is self-contained -- no network, no fonts, no frameworks -- because
 * it is delivered as a data: URI to the phone's webview and as a local file to
 * the emulator, and in neither case is there an origin to load anything from.
 *
 * Closing it differs between the two, and the page handles both:
 *   emulator  pebble-tool appends ?return_to=http://localhost:<port>/close? to
 *             the URL and reads whatever follows the first '?' of the request
 *             it then receives.
 *   phone     the Pebble app intercepts a navigation to pebblejs://close#<payload>.
 *
 * Trade-off, the same one Clay makes: the current config, secrets included, is
 * embedded in the page handed to the webview, so the secret is editable rather
 * than write-only.  That is deliberate -- an API key you cannot re-read is one
 * you cannot check a typo in -- and it is why the key is scoped to one API and
 * is cheap to rotate.
 */

'use strict';

var PAGE_CSS = [
  '*{box-sizing:border-box}',
  'body{margin:0;padding:16px;font:16px/1.4 -apple-system,Roboto,Helvetica,Arial,sans-serif;',
  '  background:#f4f4f6;color:#1a1a1a}',
  'h1{margin:0 0 4px;font-size:22px}',
  '.hint{margin:0 0 16px;color:#666;font-size:14px}',
  '.card{background:#fff;border-radius:10px;padding:12px;margin-bottom:12px;',
  '  box-shadow:0 1px 3px rgba(0,0,0,.12)}',
  '.card h2{margin:0 0 8px;font-size:15px;color:#888;font-weight:600}',
  '.field{margin-bottom:10px}',
  '.field label{display:block;font-size:13px;color:#555;margin-bottom:3px}',
  '.field .sub{font-size:12px;color:#999;margin-top:3px}',
  'input{width:100%;padding:10px;font-size:16px;border:1px solid #ccc;border-radius:6px;',
  '  background:#fff;color:#1a1a1a}',
  'input:focus{outline:none;border-color:#ff4700}',
  '.secret{display:flex;gap:8px}',
  '.secret input{flex:1}',
  'button{font-size:15px;padding:10px 14px;border-radius:6px;border:1px solid #ccc;',
  '  background:#fff;color:#1a1a1a}',
  '.tools{display:flex;gap:8px;margin-top:4px}',
  '.tools button{flex:1;padding:8px}',
  '.danger{color:#b00020;border-color:#e0a0a8}',
  '.add{width:100%;margin-bottom:16px;border-style:dashed}',
  '.bar{display:flex;gap:10px}',
  '.bar button{flex:1;padding:14px}',
  '.save{background:#ff4700;border-color:#ff4700;color:#fff;font-weight:600}',
  '.error{color:#b00020;font-size:13px;margin-top:6px}',
  '.empty{color:#666;font-size:14px;text-align:center;padding:20px 0}'
].join('\n');

/* The page script.  Kept as data so the module itself stays short; the page is
 * a document, not a program with a build step. */
var PAGE_JS = [
  '(function () {',
  '  "use strict";',
  '  var CONFIG = __RESTFORGE_CONFIG__;',
  '  var DEFAULT_HEADER = "__RESTFORGE_DEFAULT_HEADER__";',
  '  var MAX = __RESTFORGE_MAX__;',
  '',
  '  /* One row of the form.  "secret" gets a reveal toggle because a mistyped',
  '   * key is otherwise indistinguishable from a server that is down. */',
  '  var FIELDS = [',
  '    { key: "name", label: "Name", type: "text",',
  '      hint: "Shown on the watch", placeholder: "homelab" },',
  '    { key: "baseUrl", label: "Base URL", type: "url",',
  '      hint: "The Siren API root. A trailing / is added if you omit it.",',
  '      placeholder: "https://host/cgi-bin/app/" },',
  '    { key: "authHeader", label: "Auth header", type: "text",',
  '      hint: "The secret is sent in this header, never in the URL.",',
  '      placeholder: DEFAULT_HEADER },',
  '    { key: "secret", label: "Secret", type: "password",',
  '      hint: "Kept on this phone. The watch never receives it.", placeholder: "" },',
  '    { key: "startRel", label: "Start at rel", type: "text",',
  '      hint: "Optional. A link rel to open straight after the root.",',
  '      placeholder: "status" }',
  '  ];',
  '',
  '  var listEl = document.getElementById("list");',
  '  var errorEl = document.getElementById("error");',
  '',
  '  function el(tag, className, text) {',
  '    var node = document.createElement(tag);',
  '    if (className) { node.className = className; }',
  '    if (text) { node.appendChild(document.createTextNode(text)); }',
  '    return node;',
  '  }',
  '',
  '  function fieldNode(spec, backend, index) {',
  '    var wrap = el("div", "field");',
  '    var label = el("label", null, spec.label);',
  '    label.setAttribute("for", spec.key + "-" + index);',
  '    wrap.appendChild(label);',
  '',
  '    var input = document.createElement("input");',
  '    input.type = spec.type;',
  '    input.id = spec.key + "-" + index;',
  '    input.value = backend[spec.key] || "";',
  '    input.placeholder = spec.placeholder;',
  '    input.setAttribute("autocapitalize", "none");',
  '    input.setAttribute("autocorrect", "off");',
  '    input.setAttribute("spellcheck", "false");',
  '',
  '    if (spec.type === "password") {',
  '      wrap.appendChild(secretRow(input));',
  '    } else {',
  '      wrap.appendChild(input);',
  '    }',
  '    wrap.appendChild(el("div", "sub", spec.hint));',
  '    return wrap;',
  '  }',
  '',
  '  function secretRow(input) {',
  '    var row = el("div", "secret");',
  '    var toggle = el("button", null, "Show");',
  '    toggle.type = "button";',
  '    toggle.onclick = function () {',
  '      var hidden = input.type === "password";',
  '      input.type = hidden ? "text" : "password";',
  '      toggle.firstChild.nodeValue = hidden ? "Hide" : "Show";',
  '    };',
  '    row.appendChild(input);',
  '    row.appendChild(toggle);',
  '    return row;',
  '  }',
  '',
  '  function toolsNode(index) {',
  '    var tools = el("div", "tools");',
  '    tools.appendChild(button("Up", function () { move(index, -1); }));',
  '    tools.appendChild(button("Down", function () { move(index, 1); }));',
  '    var remove = button("Delete", function () {',
  '      collect();',
  '      CONFIG.splice(index, 1);',
  '      render();',
  '    });',
  '    remove.className = "danger";',
  '    tools.appendChild(remove);',
  '    return tools;',
  '  }',
  '',
  '  function button(text, onclick) {',
  '    var node = el("button", null, text);',
  '    node.type = "button";',
  '    node.onclick = onclick;',
  '    return node;',
  '  }',
  '',
  '  function cardNode(backend, index) {',
  '    var card = el("div", "card");',
  '    card.appendChild(el("h2", null, "Backend " + (index + 1)));',
  '    for (var i = 0; i < FIELDS.length; i++) {',
  '      card.appendChild(fieldNode(FIELDS[i], backend, index));',
  '    }',
  '    card.appendChild(toolsNode(index));',
  '    return card;',
  '  }',
  '',
  '  /* collect reads the DOM back into CONFIG.  Every structural change goes',
  '   * through it first, so reordering or deleting never discards edits the',
  '   * user has typed but not yet saved. */',
  '  function collect() {',
  '    for (var i = 0; i < CONFIG.length; i++) {',
  '      for (var f = 0; f < FIELDS.length; f++) {',
  '        var input = document.getElementById(FIELDS[f].key + "-" + i);',
  '        if (input) { CONFIG[i][FIELDS[f].key] = input.value; }',
  '      }',
  '    }',
  '  }',
  '',
  '  function move(index, delta) {',
  '    collect();',
  '    var to = index + delta;',
  '    if (to < 0 || to >= CONFIG.length) { return; }',
  '    var moved = CONFIG[index];',
  '    CONFIG[index] = CONFIG[to];',
  '    CONFIG[to] = moved;',
  '    render();',
  '  }',
  '',
  '  function render() {',
  '    listEl.innerHTML = "";',
  '    errorEl.innerHTML = "";',
  '    if (!CONFIG.length) {',
  '      listEl.appendChild(el("div", "empty", "No backends yet."));',
  '    }',
  '    for (var i = 0; i < CONFIG.length; i++) {',
  '      listEl.appendChild(cardNode(CONFIG[i], i));',
  '    }',
  '    document.getElementById("add").disabled = CONFIG.length >= MAX;',
  '  }',
  '',
  '  /* Mirrors validate() in settings.js.  Duplicated deliberately: the page',
  '   * cannot require() a module, and rejecting here means the user sees the',
  '   * problem next to the field instead of an empty picker on the watch. */',
  '  function problem(backend) {',
  '    if (!trim(backend.name)) { return "Name is required"; }',
  '    if (!trim(backend.baseUrl)) { return "Base URL is required"; }',
  '    if (!/^https?:\\/\\/[^\\s\\/]+\\//.test(withSlash(trim(backend.baseUrl)))) {',
  '      return "Base URL must be absolute, e.g. https://host/path/";',
  '    }',
  '    if (!trim(backend.secret)) { return "Secret is required"; }',
  '    return null;',
  '  }',
  '',
  '  function trim(value) {',
  '    return value === undefined || value === null',
  '      ? "" : String(value).replace(/^\\s+|\\s+$/g, "");',
  '  }',
  '',
  '  function withSlash(url) {',
  '    return url && url.charAt(url.length - 1) !== "/" ? url + "/" : url;',
  '  }',
  '',
  '  function closeWith(payload) {',
  '    var match = /[?&]return_to=([^&]*)/.exec(location.href);',
  '    if (match) {',
  '      location.href = decodeURIComponent(match[1]) + payload;',
  '    } else {',
  '      location.href = "pebblejs://close#" + payload;',
  '    }',
  '  }',
  '',
  '  document.getElementById("add").onclick = function () {',
  '    collect();',
  '    if (CONFIG.length >= MAX) { return; }',
  '    CONFIG.push({ name: "", baseUrl: "", authHeader: DEFAULT_HEADER,',
  '                  secret: "", startRel: "" });',
  '    render();',
  '    window.scrollTo(0, document.body.scrollHeight);',
  '  };',
  '',
  '  document.getElementById("save").onclick = function () {',
  '    collect();',
  '    errorEl.innerHTML = "";',
  '    for (var i = 0; i < CONFIG.length; i++) {',
  '      var why = problem(CONFIG[i]);',
  '      if (why) {',
  '        errorEl.appendChild(el("div", "error", "Backend " + (i + 1) + ": " + why));',
  '        return;',
  '      }',
  '    }',
  '    closeWith(encodeURIComponent(JSON.stringify(CONFIG)));',
  '  };',
  '',
  '  /* Cancel sends an empty response, which index.js treats as "leave the',
  '   * stored config alone" -- distinct from saving an empty list. */',
  '  document.getElementById("cancel").onclick = function () { closeWith(""); };',
  '',
  '  render();',
  '}());'
].join('\n');

var PAGE_BODY = [
  '<h1>RESTForge</h1>',
  '<p class="hint">Siren API backends the watch can browse. Secrets stay on this phone.</p>',
  '<div id="list"></div>',
  '<button type="button" id="add" class="add">+ Add backend</button>',
  '<div id="error"></div>',
  '<div class="bar">',
  '<button type="button" id="cancel">Cancel</button>',
  '<button type="button" id="save" class="save">Save</button>',
  '</div>'
].join('\n');

/* embed serialises a value for inclusion in an inline <script>.  JSON alone is
 * not enough: a '<' inside a string would end the inline script
 * element early, and U+2028/U+2029 are line terminators to a JS parser but not to
 * JSON.  Escaping all four as \u makes the result inert wherever it lands. */
function embed(value) {
  return JSON.stringify(value)
    .replace(/</g, '\\u003c')
    .replace(/>/g, '\\u003e')
    .replace(/\u2028/g, '\\u2028')
    .replace(/\u2029/g, '\\u2029');
}

/* html returns the complete settings page for the given backends.
 *
 * All three substitutions below use a replacer FUNCTION rather than a plain
 * replacement string. Per the ECMAScript spec, String.prototype.replace()
 * scans a string replacement argument for special '$'-patterns (dollar-dollar,
 * dollar-ampersand, dollar-backquote, dollar-quote, dollar-angle-name)
 * regardless of whether the search pattern is a string or a regex.
 * embed(backends) can contain arbitrary user-saved backend data (names,
 * URLs, secrets), so a field containing one of those would otherwise be
 * mis-substituted or -- for dollar-backquote -- splice raw page-script into
 * the output, breaking the generated <script>'s syntax. Replacer functions
 * are never subject to this interpolation, so this removes the whole bug
 * class. defaultHeader/maxBackends are fixed constants from index.js today,
 * not user data, but using the function form for all three is cheap and
 * keeps the call site uniformly safe against future changes. */
function html(backends, defaultHeader, maxBackends) {
  var script = PAGE_JS
    .replace('__RESTFORGE_CONFIG__', function () { return embed(backends || []); })
    .replace('__RESTFORGE_DEFAULT_HEADER__', function () { return String(defaultHeader); })
    .replace('__RESTFORGE_MAX__', function () { return String(maxBackends); });

  return [
    '<!DOCTYPE html>',
    '<html lang="en"><head>',
    '<meta charset="utf-8">',
    '<meta name="viewport" content="width=device-width,initial-scale=1">',
    '<title>RESTForge settings</title>',
    '<style>', PAGE_CSS, '</style>',
    '</head><body>',
    PAGE_BODY,
    '<script>', script, '</script>',
    '</body></html>'
  ].join('\n');
}

/* dataUri wraps the page for Pebble.openURL.  Percent-encoding the whole
 * document is not cosmetic: pebble-tool appends its return_to parameter after
 * the first '?' it finds, so a raw '?' or '#' anywhere in the page would split
 * the URI and silently truncate it.  Encoded, the document contains neither. */
function dataUri(backends, defaultHeader, maxBackends) {
  return 'data:text/html,' +
    encodeURIComponent(html(backends, defaultHeader, maxBackends));
}

module.exports = {
  html: html,
  dataUri: dataUri
};
