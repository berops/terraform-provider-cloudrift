package cloudriftapi

import "testing"

func Test_isImageURL(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"https://example.com/image.img": true,
		"http://example.com/image.img":  true,
		// URI schemes are case-insensitive, recipe names are not URLs.
		"HTTPS://example.com/Image.IMG": true,
		"Http://example.com/image.img":  true,
		"ubuntu":                        false,
		"ubuntu-cuda":                   false,
		"":                              false,
		"ftp://example.com/image.img":   false,
	}

	for input, want := range cases {
		if got := isImageURL(input); got != want {
			t.Errorf("isImageURL(%q) = %v, want %v", input, got, want)
		}
	}
}
