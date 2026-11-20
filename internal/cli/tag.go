package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"ticktick-cli/api"
)

// This file implements `ticktick tag` — tags:
//
//	tag list
//	tag create --name NAME --label LABEL
//
// TickTick requires tag names to be lowercase and trimmed, and the label to
// match the name when lowercased. `tag create` normalizes the name and
// validates the label before sending the request.

const tagNameMaxLen = 64

// tagFlags holds the flag values for the tag commands.
var (
	tagCreateName  string
	tagCreateLabel string
)

// tagTypeName maps the API tag type code to a label.
func tagTypeName(t int32) string {
	switch t {
	case 1:
		return "personal"
	case 2:
		return "team"
	default:
		return strconv.FormatInt(int64(t), 10)
	}
}

// tagDecodeTag decodes one tag; empty bodies yield the zero value.
func tagDecodeTag(body []byte) (api.OpenTag, error) {
	var t api.OpenTag
	if len(strings.TrimSpace(string(body))) == 0 {
		return t, nil
	}
	if err := json.Unmarshal(body, &t); err != nil {
		return t, fmt.Errorf("decode tag: %w", err)
	}
	return t, nil
}

// tagDecodeList decodes the tag array.
func tagDecodeList(body []byte) ([]api.OpenTag, error) {
	var tags []api.OpenTag
	if len(strings.TrimSpace(string(body))) == 0 {
		return tags, nil
	}
	if err := json.Unmarshal(body, &tags); err != nil {
		return nil, fmt.Errorf("decode tags: %w", err)
	}
	return tags, nil
}

// tagNormalizeRequest trims/lowercases the name and validates the label.
// It returns the wire values for name and label.
func tagNormalizeRequest(name, label string) (string, string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	label = strings.TrimSpace(label)
	if name == "" {
		return "", "", fmt.Errorf("--name must not be empty")
	}
	if utf8.RuneCountInString(name) > tagNameMaxLen {
		return "", "", fmt.Errorf("--name must be at most %d characters", tagNameMaxLen)
	}
	if label == "" {
		return "", "", fmt.Errorf("--label must not be empty")
	}
	if utf8.RuneCountInString(label) > tagNameMaxLen {
		return "", "", fmt.Errorf("--label must be at most %d characters", tagNameMaxLen)
	}
	if strings.ToLower(label) != name {
		return "", "", fmt.Errorf("--label must match --name when lowercased (got name %q, label %q)", name, label)
	}
	return name, label, nil
}

var tagListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all tags",
	Args:  cobra.NoArgs,
	Example: `  ticktick tag list
  ticktick tag list --json

Example API response:
[
  {
    "name": "work",
    "label": "work",
    "sortOrder": 0,
    "color": "#F18181",
    "type": 1
  }
]`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		resp, err := c.ListTagsWithResponse(cmd.Context())
		if err != nil {
			return err
		}
		if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
			return err
		}
		tags, err := tagDecodeList(resp.Body)
		if err != nil {
			return err
		}
		return Render(resp.Body, func(w io.Writer) {
			rows := make([][]string, 0, len(tags))
			for _, t := range tags {
				rows = append(rows, []string{
					Str(t.Name),
					Str(t.Label),
					Str(t.Color),
					tagTypeName(I32(t.Type)),
					Str(t.Parent),
				})
			}
			Table(w, []string{"NAME", "LABEL", "COLOR", "TYPE", "PARENT"}, rows)
		})
	},
}

var tagCreateCmd = &cobra.Command{
	Use:   "create --name NAME --label LABEL",
	Short: "Create a tag (name lowercase, max 64 chars; label must match name)",
	Args:  cobra.NoArgs,
	Example: `  ticktick tag create --name urgent --label urgent
  ticktick tag create --name urgent --label urgent --json

Example API response:
{
  "name": "urgent",
  "label": "urgent",
  "sortOrder": 0,
  "type": 1
}`,
	RunE: func(cmd *cobra.Command, args []string) error {
		name, label, err := tagNormalizeRequest(tagCreateName, tagCreateLabel)
		if err != nil {
			return err
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		resp, err := c.CreateTagWithResponse(cmd.Context(),
			api.TagCreateRequest{Name: name, Label: label})
		if err != nil {
			return err
		}
		if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil {
			return err
		}
		t, err := tagDecodeTag(resp.Body)
		if err != nil {
			return err
		}
		return Render(resp.Body, func(w io.Writer) {
			name2 := Str(t.Name)
			if name2 == "" {
				name2 = name
			}
			fmt.Fprintf(w, "Created tag %s\n", name2)
		})
	},
}

func init() {
	tagCreateCmd.Flags().StringVar(&tagCreateName, "name", "",
		"tag name (required, lowercase, max 64 chars)")
	tagCreateCmd.Flags().StringVar(&tagCreateLabel, "label", "",
		"tag label (required, must match name when lowercased)")
	_ = tagCreateCmd.MarkFlagRequired("name")
	_ = tagCreateCmd.MarkFlagRequired("label")

	tagCmd.AddCommand(tagListCmd, tagCreateCmd)
}
