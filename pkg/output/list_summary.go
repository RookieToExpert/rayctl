package output

import (
	"fmt"
	"io"
	"reflect"
)

type listSummary struct {
	count      int
	observed   bool
	incomplete bool
}

var activeListSummary *listSummary

// WithListSummary counts the structured results passed to list printers, before
// empty-table placeholders are added. Multiple scopes share one final footer.
func WithListSummary(writer io.Writer, render func() error) error {
	if activeListSummary != nil || IsJSON() {
		return render()
	}
	summary := &listSummary{}
	activeListSummary = summary
	defer func() { activeListSummary = nil }()
	err := render()
	if summary.observed || err == nil {
		if err != nil || summary.incomplete {
			fmt.Fprintf(writer, "\n本次输出 %d 条（查询未完整完成）。\n", summary.count)
		} else {
			fmt.Fprintf(writer, "\n本次输出 %d 条。\n", summary.count)
		}
	}
	return err
}

func recordListSummary(value any) {
	if activeListSummary == nil {
		return
	}
	count, observed := listResultRows(reflect.ValueOf(value), activeListSummary)
	activeListSummary.count += count
	activeListSummary.observed = activeListSummary.observed || observed
}

func listResultRows(value reflect.Value, summary *listSummary) (int, bool) {
	for value.IsValid() && value.Kind() == reflect.Interface {
		if value.IsNil() {
			return 0, false
		}
		value = value.Elem()
	}
	if !value.IsValid() || ((value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface) && value.IsNil()) {
		return 0, false
	}
	if value.CanInterface() {
		if result, ok := value.Interface().(interface{ IsQueryComplete() bool }); ok && !result.IsQueryComplete() {
			summary.incomplete = true
		}
	}
	for value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface {
		value = value.Elem()
		if !value.IsValid() {
			return 0, false
		}
	}
	switch value.Kind() {
	case reflect.Struct:
		if items := value.FieldByName("Items"); items.IsValid() && (items.Kind() == reflect.Slice || items.Kind() == reflect.Array) {
			return items.Len(), true
		}
	case reflect.Map:
		if value.Type().Key().Kind() == reflect.String {
			items := value.MapIndex(reflect.ValueOf("items").Convert(value.Type().Key()))
			if items.IsValid() {
				return listResultRows(items, summary)
			}
		}
	case reflect.Slice, reflect.Array:
		count := 0
		for index := 0; index < value.Len(); index++ {
			item := value.Index(index)
			if (item.Kind() == reflect.Pointer || item.Kind() == reflect.Interface) && item.IsNil() {
				continue
			}
			if rows, scope := listResultRows(item, summary); scope {
				count += rows
			} else {
				count++
			}
		}
		return count, true
	}
	return 0, false
}
