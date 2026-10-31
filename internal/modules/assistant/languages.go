package assistant

import (
	"fmt"
	"unicode"
)

// lang is a language Mango speaks besides English (always available).
type lang struct {
	Code, Name, Region string
	Script             *unicode.RangeTable
}

var languages = []lang{
	{"te", "Telugu", "Telangana", unicode.Telugu},
	{"hi", "Hindi", "North India", unicode.Devanagari},
	{"kn", "Kannada", "Karnataka", unicode.Kannada},
	{"mr", "Marathi", "Maharashtra", unicode.Devanagari},
	{"ta", "Tamil", "Tamil Nadu", unicode.Tamil},
	{"ur", "Urdu", "Hyderabad", unicode.Arabic},
}

func langByCode(code string) (lang, bool) {
	for _, l := range languages {
		if l.Code == code {
			return l, true
		}
	}
	return lang{}, false
}

// validLanguage: what the app may send as the user's choice ("" or auto =
// let speech-to-text detect it).
func validLanguage(code string) bool {
	_, ok := langByCode(code)
	return ok || code == "" || code == "auto" || code == "en"
}

// replyLang: the script the user actually wrote in decides, so English
// always gets English. Devanagari is Marathi only when Marathi was chosen.
// ok is false for English.
func replyLang(text, chosen string) (lang, bool) {
	for _, r := range text {
		for _, l := range languages {
			if unicode.In(r, l.Script) {
				if l.Script == unicode.Devanagari && chosen == "mr" {
					return langByCode("mr")
				}
				return l, true
			}
		}
	}
	return lang{}, false
}

// voiceInstructions: how a reply is read aloud. For Telugu this is the
// wording the product owner gave (Telangana style); other languages follow
// the same pattern for their region.
func voiceInstructions(l lang, ok bool) string {
	if !ok {
		return "Speak warmly and naturally, with a gentle Indian English accent, as a friendly nursery shop assistant."
	}
	return fmt.Sprintf("Speak naturally in conversational %[1]s. Use a warm, friendly Indian %[1]s speaking style, close to the %[2]s region. "+
		"Pronounce %[1]s words clearly and naturally. Handle %[1]s-English mixed sentences smoothly. Use natural pauses and human-like intonation. "+
		"Do not sound robotic or like a news reader. Speak at a moderate pace.", l.Name, l.Region)
}

// replyLanguage is the prompt's "Reply in …" clause. Whole replies in the
// language's own script, so read-aloud pronounces them in that language.
func replyLanguage(l lang, ok bool) string {
	if !ok {
		return "English, because the user wrote in English"
	}
	if l.Code == "te" {
		return "Telugu, written entirely in Telugu script: write English words in Telugu letters too (ఆర్డర్, స్టేటస్, డెలివరీ); keep codes like ORD-20261031-0001 and numbers as they are"
	}
	return fmt.Sprintf("%[1]s, written entirely in %[1]s script: write English words in %[1]s letters too; keep codes like ORD-20261031-0001 and numbers as they are", l.Name)
}
