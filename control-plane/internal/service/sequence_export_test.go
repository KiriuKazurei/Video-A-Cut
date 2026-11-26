package service

import "testing"

func TestSequenceExportPresets(t *testing.T) {
	native, err := normalizeSequenceExport(nil)
	if err != nil || !native.Native || native.ID != "program-native" {
		t.Fatalf("nil export: %+v %v", native, err)
	}
	same, err := normalizeSequenceExport(&SequenceExportOptions{PresetID: "program-native"})
	if err != nil || !same.Native {
		t.Fatalf("native preset: %+v %v", same, err)
	}
	if _, err = normalizeSequenceExport(&SequenceExportOptions{PresetID: "program-native", Codec: "h264"}); err == nil {
		t.Fatal("native preset accepted an encode override")
	}
	preset, err := normalizeSequenceExport(&SequenceExportOptions{PresetID: "mp4-720p-v1"})
	if err != nil || preset.Native || preset.MaxWidth != 1280 || preset.MaxHeight != 720 || !preset.NoUpscale || preset.Codec != "h264" || preset.CRF == nil || *preset.CRF != 20 || preset.Subtitles != "burn" {
		t.Fatalf("720p preset: %+v %v", preset, err)
	}
	vertical, err := normalizeSequenceExport(&SequenceExportOptions{PresetID: "1080x1920-vertical", Codec: "hevc", Loudness: "-14", Subtitles: "sidecar", Threads: 4})
	if err != nil || vertical.Width != 1080 || vertical.Height != 1920 || vertical.Codec != "hevc" || vertical.Loudness != "-14" || vertical.Subtitles != "sidecar" || vertical.Threads != 4 {
		t.Fatalf("vertical preset: %+v %v", vertical, err)
	}
	landscape, err := normalizeSequenceExport(&SequenceExportOptions{PresetID: "1080p-landscape"})
	if err != nil || landscape.Width != 1920 || landscape.Height != 1080 {
		t.Fatalf("1080p: %+v %v", landscape, err)
	}
	uhd, err := normalizeSequenceExport(&SequenceExportOptions{PresetID: "2160p-4k", VideoBitrate: "20M"})
	if err != nil || uhd.Width != 3840 || uhd.Height != 2160 || uhd.Bitrate != "20M" || uhd.CRF != nil {
		t.Fatalf("4k bitrate: %+v %v", uhd, err)
	}
	custom, err := normalizeSequenceExport(&SequenceExportOptions{PresetID: "custom", Width: 1080, Height: 1920})
	if err != nil || custom.Width != 1080 || custom.Height != 1920 {
		t.Fatalf("custom: %+v %v", custom, err)
	}
	for _, opt := range []*SequenceExportOptions{
		{PresetID: "missing"},
		{PresetID: "custom"},
		{PresetID: "1080p-landscape", Width: 1919, Height: 1080},
		{PresetID: "1080p-landscape", Codec: "av1"},
		{PresetID: "1080p-landscape", CRF: crfValue(20), VideoBitrate: "8M"},
		{PresetID: "1080p-landscape", Loudness: "-23"},
		{PresetID: "1080p-landscape", Subtitles: "embed"},
		{PresetID: "1080p-landscape", Threads: 32},
		{PresetID: "1080p-landscape", VideoBitrate: "fast"},
	} {
		if _, err = normalizeSequenceExport(opt); err == nil {
			t.Fatalf("accepted invalid export: %+v", opt)
		}
	}
}
