package acpserver

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/lsongdev/miya-agents/acp"
	"github.com/lsongdev/miya-agents/llm"
)

// maxInlineImage bounds a local image read into a prompt.
const maxInlineImage = 10 << 20

// promptMessage converts an ACP prompt to the user message the model sees.
//
// Text and images are passed through. Anything else, such as audio or a
// document, cannot be shown to the model directly, so it is described in
// text; when it has a location, the agent can then reach it with its tools.
func promptMessage(blocks []acp.ContentBlock) llm.Message {
	message := llm.Message{Role: llm.RoleUser}
	var texts []string
	flush := func() {
		if len(texts) > 0 {
			message.Content = append(message.Content, llm.Text(strings.Join(texts, "\n")))
			texts = nil
		}
	}
	for _, block := range blocks {
		switch {
		case block.Type == "text" || (block.Type == "" && block.Text != ""):
			if block.Text != "" {
				texts = append(texts, block.Text)
			}
		case block.Type == "image":
			if media, ok := imageMedia(block); ok {
				flush()
				message.Content = append(message.Content, llm.Block{Type: llm.BlockImage, Media: &media})
			} else {
				texts = append(texts, attachmentNote(block))
			}
		default:
			texts = append(texts, attachmentNote(block))
		}
	}
	flush()
	if len(message.Content) == 0 {
		message.Content = []llm.Block{llm.Text("")}
	}
	return message
}

// imageMedia returns the image a block carries, in a form a provider can
// accept: inline data, or an http(s) URL. A local file is read and inlined,
// since a provider cannot fetch it.
func imageMedia(block acp.ContentBlock) (llm.Media, bool) {
	media := llm.Media{Name: block.Name, MimeType: block.MimeType}
	if block.Data != "" {
		media.Data = block.Data
		media.Size = base64.StdEncoding.DecodedLen(len(block.Data))
		return media, true
	}
	if block.URI == nil {
		return media, false
	}
	location, err := url.Parse(*block.URI)
	if err != nil {
		return media, false
	}
	switch location.Scheme {
	case "http", "https":
		media.URL = *block.URI
		return media, true
	case "file":
		info, err := os.Stat(location.Path)
		if err != nil || info.IsDir() || info.Size() > maxInlineImage {
			return media, false
		}
		data, err := os.ReadFile(location.Path)
		if err != nil {
			return media, false
		}
		media.Data = base64.StdEncoding.EncodeToString(data)
		media.Size = len(data)
		return media, true
	}
	return media, false
}

// attachmentNote describes, in text, an attachment the model cannot be shown.
func attachmentNote(block acp.ContentBlock) string {
	kind := block.Type
	if kind == "resource" || kind == "resource_link" || kind == "" {
		kind = "file"
	}
	var details []string
	if block.MimeType != "" {
		details = append(details, block.MimeType)
	}
	if block.Size != nil {
		details = append(details, fmt.Sprintf("%d bytes", *block.Size))
	}
	article := "a "
	if strings.ContainsAny(kind[:1], "aeiou") {
		article = "an "
	}
	note := "[The user attached " + article + kind
	if block.Name != "" {
		note += " named " + block.Name
	}
	if len(details) > 0 {
		note += " (" + strings.Join(details, ", ") + ")"
	}
	if block.URI != nil && *block.URI != "" {
		return note + ", available at " + *block.URI + "]"
	}
	return note + "; its content was not provided to you]"
}
