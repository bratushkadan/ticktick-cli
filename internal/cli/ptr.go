package cli

// Pointer helpers for the generated API types (optional fields are pointers).

// Ptr returns a pointer to v — handy for building request bodies.
func Ptr[T any](v T) *T { return &v }

// Str dereferences an optional string ("" when nil).
func Str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// I32 dereferences an optional int32 (0 when nil).
func I32(p *int32) int32 {
	if p == nil {
		return 0
	}
	return *p
}

// I64 dereferences an optional int64 (0 when nil).
func I64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// F64 dereferences an optional float64 (0 when nil).
func F64(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

// BoolVal dereferences an optional bool (false when nil).
func BoolVal(p *bool) bool {
	if p == nil {
		return false
	}
	return *p
}

// Strs dereferences an optional string slice (nil when nil).
func Strs(p *[]string) []string {
	if p == nil {
		return nil
	}
	return *p
}