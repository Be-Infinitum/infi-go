package infi

import "iter"

// Paginate iterates over all items, following the cursor to the last page.
//
// fetch receives the cursor (nil on the first page, or whatever the caller
// wants as a starting point via PaginateFrom) and returns the items and the
// nextCursor (nil or empty on the last page). On error, the iterator yields
// the error and stops.
//
//	for p, err := range infi.Paginate(func(cursor *string) ([]infi.Payment, *string, error) {
//		l, err := client.Payments.List(ctx, infi.PaymentsListInput{Query: infi.PaymentsListQuery{Limit: infi.Ptr(50), Cursor: cursor}})
//		if err != nil {
//			return nil, nil, err
//		}
//		return l.Data, l.NextCursor, nil
//	}) {
//		if err != nil { return err }
//		_ = p
//	}
func Paginate[T any](fetch func(cursor *string) (items []T, next *string, err error)) iter.Seq2[T, error] {
	return PaginateFrom(nil, fetch)
}

// PaginateFrom is Paginate starting at an already known cursor.
func PaginateFrom[T any](start *string, fetch func(cursor *string) (items []T, next *string, err error)) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		cursor := start
		for {
			items, next, err := fetch(cursor)
			if err != nil {
				var zero T
				yield(zero, err)
				return
			}
			for _, it := range items {
				if !yield(it, nil) {
					return
				}
			}
			if next == nil || *next == "" {
				return
			}
			cursor = next
		}
	}
}
