package scriptsyntax

import "testing"

func TestShotCardReferenceKeepsMediaBindingAndNaturalText(t *testing.T) {
	doc := Parse(`{#deck-deal-flyin}{@首页截图.png}用三张卡片介绍主要功能。`)
	if len(doc.Diagnostics) != 0 || len(doc.Paragraphs) != 1 {
		t.Fatalf("shot card rejected: %+v", doc)
	}
	p := doc.Paragraphs[0]
	if p.ShotCard != "deck-deal-flyin" || p.Instruction != "用三张卡片介绍主要功能。" || len(p.References) != 1 || p.References[0] != "首页截图.png" {
		t.Fatalf("shot card interfered with the authored paragraph: %+v", p)
	}
	for _, text := range []string{`{#}`, `{#../example}`, `{#deck-deal-flyin}{#spotlight-hero-card}`} {
		if doc := Parse(text); len(doc.Diagnostics) == 0 {
			t.Fatalf("accepted invalid shot selection %s", text)
		}
	}
}

func TestNamedTimedSubtitlesKeepParagraphAndLiteralText(t *testing.T) {
	doc := Parse("介绍流程{字幕, 文本:\"第一句，{原样}\", 时间:条内1s, 持续:2s, 字号:24px}{字幕, 文本:\"第二句\", 时间:条内3s, 持续:1s}")
	if len(doc.Diagnostics) != 0 || len(doc.Paragraphs) != 1 || doc.Paragraphs[0].Instruction != "介绍流程" {
		t.Fatalf("named subtitles rejected: %+v", doc)
	}
	if cues := doc.Paragraphs[0].SubtitleCues; len(cues) != 2 || cues[0].Text != "第一句，{原样}" || cues[0].Frame != 30 || cues[0].Duration != 60 || cues[0].TimeBase != "" {
		t.Fatalf("wrong cues: %+v", cues)
	}
	doc = Parse(`{字幕, 文本:"序列字幕", 时间:序列00:02:01, 持续:1s}`)
	if len(doc.Diagnostics) != 0 || doc.Paragraphs[0].SubtitleCues[0].TimeBase != "sequence" {
		t.Fatalf("sequence time lost: %+v", doc)
	}
}

func TestDirectivesAndNaturalInstructions(t *testing.T) {
	doc := Parse("{@抽卡演示.mp4}介绍抽卡机制。{\"十次必得，规则看这里 {详情}\", 字号:24px, 字体:\"Microsoft YaHei\",}{白屏}\n\n展示本次结果{交叉溶解}")
	if len(doc.Diagnostics) != 0 || len(doc.Paragraphs) != 2 {
		t.Fatalf("%+v", doc)
	}
	p := doc.Paragraphs[0]
	if p.Video != "抽卡演示.mp4" || p.Instruction != "介绍抽卡机制。" || p.Subtitle.Text != "十次必得，规则看这里 {详情}" || p.Subtitle.SizePX != 24 || p.Subtitle.Font != "Microsoft YaHei" || p.Effect != "白屏" {
		t.Fatalf("%+v", p)
	}
	if doc.Paragraphs[1].Effect != "交叉溶解" {
		t.Fatal(doc)
	}
	for _, s := range []string{`{"字幕"}`, `{"字幕",}`, `{"字幕",字体:"Arial"}`, `{"字幕",字号:24.5px}`} {
		if d := Parse(s); len(d.Diagnostics) != 0 {
			t.Fatalf("%s: %+v", s, d)
		}
	}
}

func TestInvalidDirectivesFailClosed(t *testing.T) {
	for _, s := range []string{`{@}`, `{"x"}{"y"}`, `{"x",字号:0px}`, `{"x",字号:24}`, `{"x",字号:Infinitypx}`, `{"x",字体:""}`, `{"x",opi:24px}`, `{"x",字号:24px,字号:25px}`, `{"x",字体:Arial}`, `{未知转场}`, `{黑屏}{白屏}`, `{黑屏`, `普通文字}`, `{exec("rm")}`, `{a{b}}`, `{"unterminated}`} {
		if d := Parse(s); len(d.Diagnostics) == 0 {
			t.Fatalf("accepted %s", s)
		}
	}
}

func TestEscapesAndEmptyInput(t *testing.T) {
	anchors := Parse(`在 {@标记点} 插入 {@video}`)
	if len(anchors.Diagnostics) > 0 || len(anchors.Paragraphs[0].References) != 2 {
		t.Fatalf("typed point plus media rejected: %+v", anchors)
	}
	if d := Parse(""); len(d.Paragraphs) != 0 || len(d.Diagnostics) != 0 {
		t.Fatal(d)
	}
	d := Parse(`介绍 \{花括号\} 与 {"他说\"你好\"，然后展示 \\ 路径",}`)
	if len(d.Diagnostics) != 0 || d.Paragraphs[0].Instruction != "介绍 {花括号} 与" || d.Paragraphs[0].Subtitle.Text != "他说\"你好\"，然后展示 \\ 路径" {
		t.Fatalf("%+v", d)
	}
}
