package metasource

import (
	"reflect"
	"testing"
)

// The names are real files as Z-Library saves them, so the cases are the
// shapes that occur: an author group, a translator after the author, a second
// English title after " = ", a double space between title and subtitle, a
// country tag, and a bracket group that is an edition rather than an author.
func TestGuessFromFileName(t *testing.T) {
	for _, tc := range []struct {
		name       string
		title, alt string
		edition    string
		authors    []string
		structured bool
	}{
		{"Architecture as Code (Neal Ford, Mark Richards).epub", "Architecture as Code", "", "", []string{"Neal Ford", "Mark Richards"}, true},
		{"Dune (Frank Herbert).epub", "Dune", "", "", []string{"Frank Herbert"}, true},
		{"一读就上瘾的中国史 (温伯陵).epub", "一读就上瘾的中国史", "", "", []string{"温伯陵"}, true},
		{"亲密关系 (第8版).epub", "亲密关系", "", "第8版", nil, true},
		{"人性的弱点增订全译版 (卡耐基).epub", "人性的弱点增订全译版", "", "", []string{"卡耐基"}, true},
		{"原则 (瑞·达利欧).epub", "原则", "", "", []string{"瑞·达利欧"}, true},
		{"基督山伯爵 (大仲马).epub", "基督山伯爵", "", "", []string{"大仲马"}, true},
		{"大空头 ([美]Michael Lewis).epub", "大空头", "", "", []string{"Michael Lewis"}, true},
		{"探秘古埃及 = Ancient Egypt A Very Short Introduction ([英] 伊恩・肖 (Ian Shaw) 著  颜海英 译).azw3",
			"探秘古埃及", "Ancient Egypt A Very Short Introduction", "", []string{"伊恩·肖"}, true},
		{"深度思考不断逼近问题的本质 (莫琳·希凯, 孔锐才).epub", "深度思考不断逼近问题的本质", "", "", []string{"莫琳·希凯", "孔锐才"}, true},
		{"看人的艺术  11种以物识人术, 看人看到骨子里 = Snoop What Your Stuff Says About You ([美] 山姆 · 高斯林 (Sam Gosling) 著  宋媛媛 译).epub",
			"看人的艺术: 11种以物识人术, 看人看到骨子里", "Snoop What Your Stuff Says About You", "", []string{"山姆·高斯林"}, true},
		{"被讨厌的勇气 (岸見一郎  古賀史健).epub", "被讨厌的勇气", "", "", []string{"岸見一郎", "古賀史健"}, true},
		{"鬼吹灯全集 (天下霸唱).epub", "鬼吹灯全集", "", "", []string{"天下霸唱"}, true},

		// Downloader noise after the author group is not the author group.
		{"Dune (Frank Herbert) (Z-Library).epub", "Dune", "", "", []string{"Frank Herbert"}, true},
		{"Dune (Frank Herbert) (1).epub", "Dune", "", "", []string{"Frank Herbert"}, true},
		{"三体 (刘慈欣) (z-lib.org).mobi", "三体", "", "", []string{"刘慈欣"}, true},
		// Full-width brackets, and an edition group before the author group.
		{"三体（刘慈欣）.epub", "三体", "", "", []string{"刘慈欣"}, true},
		{"Python Cookbook (3rd edition) (David Beazley, Brian K. Jones).pdf", "Python Cookbook (3rd edition)", "", "", []string{"David Beazley", "Brian K. Jones"}, true},
		{"Dune (2nd edition).epub", "Dune", "", "2nd edition", nil, true},
		// Several translators after "译", and an editor role.
		{"某书 (A 著 B, C 译).epub", "某书", "", "", []string{"A"}, true},
		{"论文集 (张三 主编).epub", "论文集", "", "", []string{"张三"}, true},

		// No author group: the whole name is a title, with no structure to trust.
		{"Dune.epub", "Dune", "", "", nil, false},
		{"Dune - Frank Herbert.epub", "Dune - Frank Herbert", "", "", nil, false},
		{"no extension", "no extension", "", "", nil, false},
	} {
		g := GuessFromFileName(tc.name)
		if g.Title != tc.title || g.AltTitle != tc.alt || g.Edition != tc.edition || !reflect.DeepEqual(g.Authors, tc.authors) || g.Structured != tc.structured {
			t.Errorf("%s:\n got title=%q alt=%q edition=%q authors=%q structured=%v\nwant title=%q alt=%q edition=%q authors=%q structured=%v",
				tc.name, g.Title, g.AltTitle, g.Edition, g.Authors, g.Structured, tc.title, tc.alt, tc.edition, tc.authors, tc.structured)
		}
	}
}

func TestGuessFromFileNameFindsAnISBNInTheName(t *testing.T) {
	for name, want := range map[string][]string{
		"9787536692930.epub":                         {"9787536692930"},
		"978-7-5366-9293-0.epub":                     {"9787536692930"},
		"三体 (刘慈欣) 9787536692930.epub":                {"9787536692930"},
		"三体 ISBN 7-5366-9293-5.epub":                 {"9787536692930"},
		"Dune (Frank Herbert).epub":                  nil,
		"Vol 2024 report 20240115.epub":              nil, // digits that are not an ISBN
		"9787536692931.epub":                         nil, // bad check digit
		"一读就上瘾的中国史 (温伯陵) 572079.epub":                nil,
		"三体 9787536692930 9780140328721.epub":        {"9787536692930", "9780140328721"},
		"三体 (刘慈欣) [9787536692930].epub":              {"9787536692930"},
		"三体 (刘慈欣) 9787536692930 (Z-Library).epub":    {"9787536692930"},
		"a 97875366929309787536692930 b.epub":        nil, // one long run is not two ISBNs
		"2024-01-15 9787536692930 notes (Me).epub":   {"9787536692930"},
		"9787536692930 (刘慈欣).epub":                   {"9787536692930"},
		"7536692935.epub":                            {"9787536692930"},
		"phone 13800138000 9787536692930.epub":       {"9787536692930"},
		"9787536692930X.epub":                        nil,
		"Chapter 1 2 3 4 5 6 7 8 9 10 11 12 13.epub": nil,
	} {
		got := GuessFromFileName(name).ISBNs
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: ISBNs = %v, want %v", name, got, want)
		}
	}
}

// An ISBN or a bare number is not a title, so the name offers none.
func TestGuessFromFileNameDoesNotTakeAnISBNForATitle(t *testing.T) {
	for _, name := range []string{"9787536692930.epub", "978-7-5366-9293-0.pdf", "7536692935.mobi", "20240115.epub", "12345.epub"} {
		if g := GuessFromFileName(name); g.Title != "" || g.Structured {
			t.Errorf("%s: title = %q structured = %v, want no title", name, g.Title, g.Structured)
		}
	}
}

// Words that name a kind of file, or a program's placeholder, are not titles.
func TestUsefulTitle(t *testing.T) {
	for title, want := range map[string]bool{
		"三体": true, "Dune": true, "原则": true, "A": false, "": false, "  ": false,
		"Unknown": false, "unknown": false, "未知": false, "Untitled": false, "无标题": false, "未命名": false,
		"Microsoft Word - report.doc": false, "Title": false, "Document": false, "book": false, "ebook": false,
		"0f1e2d3c-0000-4000-8000-000000000001": false, "9787536692930": false, "12345": false, "新建文档": false,
		"The Unknown Soldier": true, "Unknown Pleasures": true, "Book of Dust": true, "文档管理": true,
		"report.pdf": false, "draft.indd": false,
	} {
		if got := UsefulTitle(title); got != want {
			t.Errorf("UsefulTitle(%q) = %v, want %v", title, got, want)
		}
	}
}
