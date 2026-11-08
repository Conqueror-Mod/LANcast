package media

import (
	"reflect"
	"testing"
)

// One tag, the people it names (ADR 0116 step 3).
func TestSplitPeople(t *testing.T) {
	cases := map[string][]string{
		"Leonard Nimoy":                    {"Leonard Nimoy"},
		"Nimoy, Leonard":                   {"Leonard Nimoy"},
		"Leonard Nimoy, John de Lancie":    {"Leonard Nimoy", "John de Lancie"},
		"Leonard Nimoy; John de Lancie":    {"Leonard Nimoy", "John de Lancie"},
		"Leonard Nimoy & John de Lancie":   {"Leonard Nimoy", "John de Lancie"},
		"Leonard Nimoy and John de Lancie": {"Leonard Nimoy", "John de Lancie"},
		"Read by Kate Mulgrew":             {"Kate Mulgrew"},
		"Narrated by Nimoy, Leonard":       {"Leonard Nimoy"},
		"A. Actor, B. Actor, C. Actor":     {"A. Actor", "B. Actor", "C. Actor"},
		"Leonard Nimoy / George Takei":     {"Leonard Nimoy", "George Takei"},
		"":                                 nil,
		"   ":                              nil,
	}
	for in, want := range cases {
		if got := splitPeople(in); !reflect.DeepEqual(got, want) {
			t.Errorf("splitPeople(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBookNarrators(t *testing.T) {
	part := func(composer, narrator string) BookFile {
		return BookFile{Tags: BookTags{Composer: composer, Narrator: narrator}}
	}

	// The composer tag, where rippers put the reader.
	got := bookNarrators([]BookFile{part("Leonard Nimoy", ""), part("Leonard Nimoy", "")})
	if !reflect.DeepEqual(got, []string{"Leonard Nimoy"}) {
		t.Errorf("composer only: %q", got)
	}

	// A narrator tag on any part wins over every composer tag.
	got = bookNarrators([]BookFile{part("Some Composer", ""), part("Some Composer", "Kate Mulgrew")})
	if !reflect.DeepEqual(got, []string{"Kate Mulgrew"}) {
		t.Errorf("narrator tag: %q", got)
	}

	// A full cast: everybody, the most-credited first, a guest on one part last.
	got = bookNarrators([]BookFile{
		part("Patrick Stewart; Jonathan Frakes", ""),
		part("Patrick Stewart; Jonathan Frakes; Whoopi Goldberg", ""),
		part("Jonathan Frakes, Patrick Stewart", ""),
	})
	want := []string{"Patrick Stewart", "Jonathan Frakes", "Whoopi Goldberg"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("full cast: %q, want %q", got, want)
	}

	// Nobody named is nobody, not an empty name.
	if got := bookNarrators([]BookFile{part("", "")}); len(got) != 0 {
		t.Errorf("untagged: %q", got)
	}
}
