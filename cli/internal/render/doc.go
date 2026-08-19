// Package render turns a siren.Entity into rows for a UI.
//
// This is the Go port of flutter/lib/services/render_service.dart, itself
// a port of pebble/src/pkjs/render.js -- see that file's header and
// docs/DESIGN.md ("Rendering does not interpret") for the reasoning this
// package exists to keep. A Siren entity has four renderable parts, and
// Document renders them in the order the specification lists them --
// properties, sub-entities, links, actions. Choosing a different order
// would mean deciding which part of somebody else's document matters
// most, which is exactly the knowledge this app is built not to have.
//
// Two things this package deliberately does not do:
//
//   - It does not interpret a property. A value is shown as the server
//     sent it, so three states never collapse into two -- see Text. A
//     client that renders "ping: false" as "off" is asserting something
//     the server did not say.
//   - It does not hide anything. Every link, action and property is
//     offered, including the ones this app has no idea about, because
//     "I do not recognise this" is not a reason to withhold it from the
//     person using it.
//
// Each Row carries a RowTarget, which is what a caller does when the row
// is activated. The UI layer reads a row's label, sublabel and kind, and
// hands the target back to whichever code knows what it means -- it never
// inspects the target's internals itself.
//
// One thing this package does that neither the JS nor the Dart port needs
// to: siren.Entity.Properties is a plain Go map, decoded generically by
// encoding/json, which keeps no record of the order the server wrote its
// properties in -- unlike a JS object or a Dart LinkedHashMap, which both
// preserve insertion order. Go additionally randomises map iteration order
// by design. So wherever propertyRows, summarise and Describe below need a
// property order, they sort the keys instead of ranging over the map
// directly; see sortedPropertyKeys in rows.go for the full reasoning. This
// is a deliberate, documented departure from the literal behaviour of the
// two other ports, forced by the shape of the shared siren.Entity type,
// not an oversight.
package render
