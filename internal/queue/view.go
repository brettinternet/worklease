package queue

import "sort"

func EvaluateView(items map[string]Item, view View) []Item {
	order := map[string]int{}
	for i, id := range view.SourceOrder {
		if _, ok := order[id]; !ok {
			order[id] = i
		}
	}
	result := make([]Item, 0)
	for _, item := range items {
		if _, configured := order[item.Ref.SourceID]; configured && matches(item, view.Filters) {
			result = append(result, cloneItem(item))
		}
	}
	less := func(a, b Item) bool {
		ai, aok := order[a.Ref.SourceID]
		bi, bok := order[b.Ref.SourceID]
		if aok != bok {
			return aok
		}
		if ai != bi {
			return ai < bi
		}
		if a.Order != b.Order {
			if a.Order == "" {
				return false
			}
			if b.Order == "" {
				return true
			}
			return a.Order < b.Order
		}
		return a.Ref.Less(b.Ref)
	}
	sort.SliceStable(result, func(i, j int) bool { return less(result[i], result[j]) })
	canonicalSeen := map[string]bool{}
	deduplicated := result[:0]
	for _, item := range result {
		id := canonical(item)
		if canonicalSeen[id] {
			continue
		}
		canonicalSeen[id] = true
		deduplicated = append(deduplicated, item)
	}
	return deduplicated
}
func matches(i Item, f Filters) bool {
	if len(f.SourceIDs) > 0 {
		found := false
		for _, id := range f.SourceIDs {
			if i.Ref.SourceID == id {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if len(f.States) > 0 {
		found := false
		for _, s := range f.States {
			if i.State == s {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if f.Text != "" {
		if !containsFold(i.Title, f.Text) && !containsFold(i.Body, f.Text) {
			return false
		}
	}
	return true
}
func containsFold(s, q string) bool { return len(q) == 0 || len(s) >= len(q) && indexFold(s, q) >= 0 }
func indexFold(s, q string) int {
	a, b := []rune(s), []rune(q)
	for i := 0; i+len(b) <= len(a); i++ {
		ok := true
		for j := range b {
			x, y := a[i+j], b[j]
			if x >= 'A' && x <= 'Z' {
				x += 32
			}
			if y >= 'A' && y <= 'Z' {
				y += 32
			}
			if x != y {
				ok = false
				break
			}
		}
		if ok {
			return i
		}
	}
	return -1
}
