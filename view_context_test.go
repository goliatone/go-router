package router

import (
	"errors"
	"io"
	"testing"
)

type testViewContextSerializer struct {
	payload string
	err     error
}

func (s testViewContextSerializer) Serialize() ([]byte, error) {
	if s.err != nil {
		return nil, s.err
	}
	return []byte(s.payload), nil
}

type testViewContextPayload struct {
	PageTitle string                 `json:"page_title"`
	Line      int                    `json:"line"`
	Price     float64                `json:"price"`
	Nested    testViewContextNested  `json:"nested"`
	Items     []testViewContextItem  `json:"items"`
	Attrs     map[string]interface{} `json:"attrs"`
}

type testViewContextNested struct {
	Number int     `json:"number"`
	Ratio  float64 `json:"ratio"`
}

type testViewContextItem struct {
	Line  int     `json:"line"`
	Score float64 `json:"score"`
}

func TestSerializeAsContext_UsesJSONTagsAndPreservesNumericKinds(t *testing.T) {
	input := testViewContextPayload{
		PageTitle: "Example",
		Line:      42,
		Price:     10.5,
		Nested: testViewContextNested{
			Number: 9,
			Ratio:  3.25,
		},
		Items: []testViewContextItem{
			{Line: 7, Score: 1.5},
			{Line: 11, Score: 2.75},
		},
		Attrs: map[string]interface{}{
			"number": 12,
			"ratio":  0.5,
			"list":   []interface{}{1, 2.5},
		},
	}

	ctx, err := SerializeAsContext(input)
	if err != nil {
		t.Fatalf("SerializeAsContext returned error: %v", err)
	}

	if _, ok := ctx["page_title"]; !ok {
		t.Fatal("expected key page_title from json tag")
	}
	if _, ok := ctx["PageTitle"]; ok {
		t.Fatal("did not expect key PageTitle")
	}

	topLine, ok := ctx["line"].(int64)
	if !ok || topLine != 42 {
		t.Fatalf("expected top-level line int64(42), got %T(%v)", ctx["line"], ctx["line"])
	}

	price, ok := ctx["price"].(float64)
	if !ok || price != 10.5 {
		t.Fatalf("expected price float64(10.5), got %T(%v)", ctx["price"], ctx["price"])
	}

	nested, ok := ctx["nested"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested to be map[string]any, got %T", ctx["nested"])
	}

	nestedNumber, ok := nested["number"].(int64)
	if !ok || nestedNumber != 9 {
		t.Fatalf("expected nested.number int64(9), got %T(%v)", nested["number"], nested["number"])
	}

	nestedRatio, ok := nested["ratio"].(float64)
	if !ok || nestedRatio != 3.25 {
		t.Fatalf("expected nested.ratio float64(3.25), got %T(%v)", nested["ratio"], nested["ratio"])
	}

	items, ok := ctx["items"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("expected items []any of len 2, got %T len=%d", ctx["items"], len(items))
	}

	firstItem, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("expected items[0] to be map[string]any, got %T", items[0])
	}

	itemLine, ok := firstItem["line"].(int64)
	if !ok || itemLine != 7 {
		t.Fatalf("expected items[0].line int64(7), got %T(%v)", firstItem["line"], firstItem["line"])
	}

	itemScore, ok := firstItem["score"].(float64)
	if !ok || itemScore != 1.5 {
		t.Fatalf("expected items[0].score float64(1.5), got %T(%v)", firstItem["score"], firstItem["score"])
	}

	attrs, ok := ctx["attrs"].(map[string]any)
	if !ok {
		t.Fatalf("expected attrs to be map[string]any, got %T", ctx["attrs"])
	}

	attrNumber, ok := attrs["number"].(int64)
	if !ok || attrNumber != 12 {
		t.Fatalf("expected attrs.number int64(12), got %T(%v)", attrs["number"], attrs["number"])
	}

	attrRatio, ok := attrs["ratio"].(float64)
	if !ok || attrRatio != 0.5 {
		t.Fatalf("expected attrs.ratio float64(0.5), got %T(%v)", attrs["ratio"], attrs["ratio"])
	}

	attrList, ok := attrs["list"].([]any)
	if !ok || len(attrList) != 2 {
		t.Fatalf("expected attrs.list []any len=2, got %T len=%d", attrs["list"], len(attrList))
	}

	if listItem, ok := attrList[0].(int64); !ok || listItem != 1 {
		t.Fatalf("expected attrs.list[0] int64(1), got %T(%v)", attrList[0], attrList[0])
	}
	if listItem, ok := attrList[1].(float64); !ok || listItem != 2.5 {
		t.Fatalf("expected attrs.list[1] float64(2.5), got %T(%v)", attrList[1], attrList[1])
	}
}

func TestSerializeAsContext_UsesSerializerAndNormalizesNumbers(t *testing.T) {
	input := testViewContextSerializer{
		payload: `{"line":42,"ratio":2.5,"nested":{"number":7},"items":[{"number":3},2]}`,
	}

	ctx, err := SerializeAsContext(input)
	if err != nil {
		t.Fatalf("SerializeAsContext returned error: %v", err)
	}

	if line, ok := ctx["line"].(int64); !ok || line != 42 {
		t.Fatalf("expected line int64(42), got %T(%v)", ctx["line"], ctx["line"])
	}
	if ratio, ok := ctx["ratio"].(float64); !ok || ratio != 2.5 {
		t.Fatalf("expected ratio float64(2.5), got %T(%v)", ctx["ratio"], ctx["ratio"])
	}

	nested, ok := ctx["nested"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested map, got %T", ctx["nested"])
	}
	if number, ok := nested["number"].(int64); !ok || number != 7 {
		t.Fatalf("expected nested.number int64(7), got %T(%v)", nested["number"], nested["number"])
	}

	items, ok := ctx["items"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("expected items []any len=2, got %T len=%d", ctx["items"], len(items))
	}

	first, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("expected items[0] map, got %T", items[0])
	}
	if number, ok := first["number"].(int64); !ok || number != 3 {
		t.Fatalf("expected items[0].number int64(3), got %T(%v)", first["number"], first["number"])
	}
	if second, ok := items[1].(int64); !ok || second != 2 {
		t.Fatalf("expected items[1] int64(2), got %T(%v)", items[1], items[1])
	}
}

func TestSerializeAsContext_ReturnsErrorForTrailingJSON(t *testing.T) {
	input := testViewContextSerializer{
		payload: `{"line":1}{"line":2}`,
	}

	_, err := SerializeAsContext(input)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("expected trailing JSON error %v, got %v", io.ErrUnexpectedEOF, err)
	}
}

func TestSerializeAsContext_NilInputReturnsEmptyMap(t *testing.T) {
	ctx, err := SerializeAsContext(nil)
	if err != nil {
		t.Fatalf("SerializeAsContext returned error: %v", err)
	}
	if ctx == nil {
		t.Fatal("expected non-nil map")
	}
	if len(ctx) != 0 {
		t.Fatalf("expected empty map, got len=%d", len(ctx))
	}
}

func TestSerializeAsContext_PropagatesSerializerError(t *testing.T) {
	expectedErr := errors.New("serialize failed")
	input := testViewContextSerializer{err: expectedErr}

	_, err := SerializeAsContext(input)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected serializer error %v, got %v", expectedErr, err)
	}
}
