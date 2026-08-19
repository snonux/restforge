package quick

// Count returns the number of stored shortcuts.
func Count() (int, error) {
	list, err := Load()
	if err != nil {
		return 0, err
	}
	return len(list), nil
}

// Get returns the shortcut at index, or nil, nil when index is out of
// range -- a real "there is nothing there" answer, not an error. Mirrors
// quick_service.dart's get()/quick.js's get().
func Get(index int) (*QuickItem, error) {
	list, err := Load()
	if err != nil {
		return nil, err
	}
	if index < 0 || index >= len(list) {
		return nil, nil
	}
	item := list[index]
	return &item, nil
}

// Add appends item, replacing any existing shortcut with the same target
// (see same) rather than accumulating duplicates -- saving the same row
// twice is a natural thing to do and should be idempotent, with the newer
// label winning. Returns nil, nil -- refusing to save, not an error --
// when item is incomplete (fails usable) or the list is already at
// MaxQuick; nil, non-nil error only when Save itself failed. Never reports
// a shortcut as saved when nothing was persisted. Mirrors
// quick_service.dart's add()/quick.js's add().
func Add(item QuickItem) (*QuickItem, error) {
	wanted := Normalise(item)
	if !usable(wanted) {
		Warnf("refusing to save an incomplete shortcut")
		return nil, nil
	}

	list, err := Load()
	if err != nil {
		return nil, err
	}

	if idx := indexOfSame(list, wanted); idx != -1 {
		updated := append([]QuickItem(nil), list...)
		updated[idx] = wanted
		return persist(updated, wanted)
	}

	if len(list) >= MaxQuick {
		Warnf("already holding %d shortcuts", MaxQuick)
		return nil, nil
	}
	return persist(append(list, wanted), wanted)
}

// indexOfSame returns the index of the first entry in list that same()
// matches wanted's target, or -1 when none does.
func indexOfSame(list []QuickItem, wanted QuickItem) int {
	for i, stored := range list {
		if same(stored, wanted) {
			return i
		}
	}
	return -1
}

// persist saves list and, on success, reports wanted as the item that was
// saved -- the single exit path Add's two branches (update-in-place,
// append) share.
func persist(list []QuickItem, wanted QuickItem) (*QuickItem, error) {
	if _, err := Save(list); err != nil {
		return nil, err
	}
	item := wanted
	return &item, nil
}

// RemoveItem drops the stored shortcut whose target matches item (see
// same), rather than a position -- the caller-facing counterpart to Remove
// for a caller that has an item in hand but not necessarily its position in
// Load's own slice. Needed because a caller driving a UI over a filtered or
// reordered view of the stored list (a terminal's fuzzy-filtered
// bubbles/list.Model, for one) has no reliable position to pass Remove at
// all: list.Model.Index() is the cursor's position within whatever subset
// is currently displayed, not within the underlying stored order Load/Save
// use, so translating one into the other would either require duplicating
// this package's own filtering/ordering or would silently remove the wrong
// entry the moment a filter narrows the list. Going through same() instead
// sidesteps the mismatch entirely: whichever entry the caller is actually
// looking at is exactly the one removed, independent of anything about how
// it currently happens to be displayed. Same return contract as Remove:
// false, nil when nothing matched; false, non-nil error only when Save
// itself failed.
func RemoveItem(item QuickItem) (bool, error) {
	list, err := Load()
	if err != nil {
		return false, err
	}
	idx := indexOfSame(list, item)
	if idx == -1 {
		return false, nil
	}
	return removeAt(list, idx)
}

// Remove drops the shortcut at index. Returns false, nil -- refusing, not
// an error -- when index is out of range; false, non-nil error only when
// Save itself failed, so a caller never reports a removal that did not
// happen. Mirrors quick_service.dart's remove()/quick.js's remove().
func Remove(index int) (bool, error) {
	list, err := Load()
	if err != nil {
		return false, err
	}
	if index < 0 || index >= len(list) {
		return false, nil
	}
	return removeAt(list, index)
}

// removeAt drops list[index] and saves the result -- the shared tail of
// Remove and RemoveItem, both of which have already loaded list and
// resolved index within it by the time they call this.
func removeAt(list []QuickItem, index int) (bool, error) {
	updated := make([]QuickItem, 0, len(list)-1)
	updated = append(updated, list[:index]...)
	updated = append(updated, list[index+1:]...)

	if _, err := Save(updated); err != nil {
		return false, err
	}
	return true, nil
}
