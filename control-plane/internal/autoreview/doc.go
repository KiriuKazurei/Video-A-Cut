// Package autoreview is the rule-based self-assessment of an auto run: it turns
// facts about a finished export (scenes, narration, audio, subtitles, preview
// report) into a band (low, medium, high), a score, per-item components and a
// list of fixed reason codes.
//
// The package is pure. It reads no files, calls no network and has no clock; the
// same Input and Thresholds always give the same Result, byte for byte once
// marshalled. Collecting the facts from storage is the caller's job (see
// FromEDL for the part that only needs the parsed EDL).
//
// The score is a reference for the person, not an acceptance: it never grants
// anything the user did not authorize and a HIGH band is the only one the
// driver may complete on its own. Anything the scorer cannot compute makes the
// result fail safe: the missing item contributes zero and a warn-level reason
// blocks HIGH.
package autoreview
