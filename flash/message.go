package flash

import (
	"fmt"
	"strings"

	"github.com/goliatone/go-router"
)

const (
	toastCountKey = "toast_count"
	toastTypeKey  = "toast_type"
	toastTitleKey = "toast_title"
	toastTextKey  = "toast_text"
)

type Message struct {
	Type  string
	Title string
	Text  string
}

func (f *Flash) SetMessage(c router.Context, msg Message) router.Context {
	msg = f.applyMessageDefaults(msg)
	current := f.mergePending(c, router.ViewContext{})
	next := addMessageToContext(current, msg)
	f.setCookie(c, next)
	return c
}

func (f *Flash) GetMessage(c router.Context) (*Message, bool) {
	data := f.Get(c)
	return GetMessageFrom(data)
}

func (f *Flash) GetMessages(c router.Context) ([]Message, bool) {
	data := f.Get(c)
	return GetMessagesFrom(data)
}

func SetMessage(c router.Context, msg Message) router.Context {
	return DefaultFlash.SetMessage(c, msg)
}

func GetMessage(c router.Context) (*Message, bool) {
	// Prefer middleware-injected data when available.
	if v := c.Locals("flash"); v != nil {
		if flashData, ok := v.(router.ViewContext); ok {
			return GetMessageFrom(flashData)
		}
	}
	return DefaultFlash.GetMessage(c)
}

func GetMessages(c router.Context) ([]Message, bool) {
	// Prefer middleware-injected data when available.
	if v := c.Locals("flash"); v != nil {
		if flashData, ok := v.(router.ViewContext); ok {
			return GetMessagesFrom(flashData)
		}
	}
	return DefaultFlash.GetMessages(c)
}

func GetMessageFrom(data router.ViewContext) (*Message, bool) {
	msgs, ok := GetMessagesFrom(data)
	if !ok || len(msgs) == 0 {
		return nil, false
	}
	return &msgs[0], true
}

func GetMessagesFrom(data router.ViewContext) ([]Message, bool) {
	if data == nil {
		return nil, false
	}

	// Backwards/compat: accept single-message keys.
	if _, ok := data[toastCountKey]; !ok {
		if t, ok := getString(data, toastTypeKey); ok && t != "" {
			title, _ := getString(data, toastTitleKey)
			text, _ := getString(data, toastTextKey)
			return []Message{{Type: t, Title: title, Text: text}}, true
		}
	}

	count, ok := getInt(data, toastCountKey)
	if !ok || count <= 0 {
		return nil, false
	}

	out := make([]Message, 0, count)
	for i := 0; i < count; i++ {
		t, _ := getString(data, fmt.Sprintf("toast_%d_type", i))
		title, _ := getString(data, fmt.Sprintf("toast_%d_title", i))
		text, _ := getString(data, fmt.Sprintf("toast_%d_text", i))
		if t == "" && title == "" && text == "" {
			continue
		}
		out = append(out, Message{Type: t, Title: title, Text: text})
	}

	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

func (f *Flash) applyMessageDefaults(msg Message) Message {
	if msg.Title == "" {
		msg.Title = f.config.DefaultMessageTitle
	}
	if msg.Text == "" {
		msg.Text = f.config.DefaultMessageText
	}
	return msg
}

func addMessageToContext(data router.ViewContext, msg Message) router.ViewContext {
	out := cloneViewContext(data)

	// Decode existing messages, append, then re-encode (so we can safely support multiple).
	existing, _ := GetMessagesFrom(out)
	existing = append(existing, msg)

	// Remove any existing toast keys (single or multi).
	for k := range out {
		if k == toastCountKey || k == toastTypeKey || k == toastTitleKey || k == toastTextKey || strings.HasPrefix(k, "toast_") {
			delete(out, k)
		}
	}

	out[toastCountKey] = fmt.Sprintf("%d", len(existing))
	for i, m := range existing {
		out[fmt.Sprintf("toast_%d_type", i)] = m.Type
		out[fmt.Sprintf("toast_%d_title", i)] = m.Title
		out[fmt.Sprintf("toast_%d_text", i)] = m.Text
	}
	return out
}

