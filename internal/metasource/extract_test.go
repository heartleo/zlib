package metasource

import (
	"reflect"
	"testing"

	"github.com/heartleo/zlib/internal/ebookmeta"
)

// realBook is one of the user's real files: its name, and what is embedded in
// it, as `zlib meta --json` reported it.
type realBook struct {
	name       string
	embTitle   string
	embAuthors []string
	embISBN    string
	publisher  string
	published  string
	// What the query should be.
	title      string
	authors    []string
	isbns      []string
	titleFrom  string
	guessed    bool
	authorFrom string
}

func realBooks() []realBook {
	return []realBook{
		{name: "Architecture as Code (Neal Ford, Mark Richards).epub", embTitle: "Architecture as Code", embAuthors: []string{"Neal Ford and Mark Richards"},
			embISBN: "9798341640382", publisher: "O'Reilly Media, Inc.", published: "2025-12-15",
			title: "Architecture as Code", authors: []string{"Neal Ford", "Mark Richards"}, isbns: []string{"9798341640382"}, titleFrom: "file name", authorFrom: "embedded"},
		{name: "Dune (Frank Herbert).epub", embTitle: "Dune", embAuthors: []string{"Frank Herbert"}, embISBN: "9781101658055", publisher: "Penguin Publishing Group", published: "2003-08-26",
			title: "Dune", authors: []string{"Frank Herbert"}, isbns: []string{"9781101658055"}, titleFrom: "file name", authorFrom: "embedded"},
		{name: "一读就上瘾的中国史 (温伯陵).epub", embTitle: "一读就上瘾的中国史(爆款历史大号“温乎”首部作品集,572079个读者共同的选择)", embAuthors: []string{"温伯陵"}, published: "2020-08-31T16:00:00+00:00",
			title: "一读就上瘾的中国史", authors: []string{"温伯陵"}, titleFrom: "file name", authorFrom: "embedded"},
		{name: "亲密关系 (第8版).epub", embTitle: "Unknown", embAuthors: []string{"Unknown"},
			title: "亲密关系", titleFrom: "file name", guessed: true},
		{name: "人性的弱点增订全译版 (卡耐基).epub", embTitle: "《人性的弱点：增订全译版》", embAuthors: []string{"[美]戴尔.卡耐基"}, publisher: "湖南文艺出版社", published: "2016-04-01T05:00:00+00:00",
			title: "人性的弱点增订全译版", authors: []string{"戴尔·卡耐基"}, titleFrom: "file name", authorFrom: "embedded"},
		{name: "原则 (瑞·达利欧).epub", embTitle: "原则", embAuthors: []string{"瑞·达利欧"}, embISBN: "9787508684031", publisher: "中信出版社", published: "2018-01-01",
			title: "原则", authors: []string{"瑞·达利欧"}, isbns: []string{"9787508684031"}, titleFrom: "file name", authorFrom: "embedded"},
		{name: "基督山伯爵 (大仲马).epub", embTitle: "读客经典文库：基督山伯爵（余华不吃不喝不睡，疯了般读完《基督山伯爵》！人类全部的智慧尽在其中！全三册一字未删完整版！）", embAuthors: []string{"大仲马"},
			title: "基督山伯爵", authors: []string{"大仲马"}, titleFrom: "file name", authorFrom: "embedded"},
		// The embedded ISBN has a bad check digit, so it is not used; the author
		// is the one in the book's own script, as the sources list it.
		{name: "大空头 ([美]Michael Lewis).epub", embTitle: "大空头", embAuthors: []string{"（美）刘易斯"}, embISBN: "2774693437", publisher: "中信出版集团股份有限公司", published: "2015-05-01",
			title: "大空头", authors: []string{"刘易斯"}, titleFrom: "file name", authorFrom: "embedded"},
		{name: "探秘古埃及 = Ancient Egypt A Very Short Introduction ([英] 伊恩・肖 (Ian Shaw) 著  颜海英 译).azw3", embTitle: "探秘古埃及 (百科通识文库)", embAuthors: []string{"伊恩·肖"}, publisher: "外语教学与研究出版社", published: "2015-08-01",
			title: "探秘古埃及", authors: []string{"伊恩·肖"}, titleFrom: "file name", authorFrom: "embedded"},
		// The name lists the translator too; the embedded author does not.
		{name: "深度思考不断逼近问题的本质 (莫琳·希凯, 孔锐才).epub", embTitle: "深度思考：不断逼近问题的本质", embAuthors: []string{"莫琳·希凯"}, published: "2018-08-31T16:00:00+00:00",
			title: "深度思考不断逼近问题的本质", authors: []string{"莫琳·希凯"}, titleFrom: "file name", authorFrom: "embedded"},
		{name: "看人的艺术  11种以物识人术, 看人看到骨子里 = Snoop What Your Stuff Says About You ([美] 山姆 · 高斯林 (Sam Gosling) 著  宋媛媛 译).epub",
			embTitle: "看人的艺术【罗辑思维推荐，打动罗胖的好书。“美国心理学会年度贡献奖”，“New Scientist年度好书”，《纽约时报》《华盛顿邮报》《今日心理》《早安美国》一致推荐】", embAuthors: []string{"山姆·高斯林"}, embISBN: "1510159946", publisher: "北京联合出版公司",
			title: "看人的艺术: 11种以物识人术, 看人看到骨子里", authors: []string{"山姆·高斯林"}, titleFrom: "file name", authorFrom: "embedded"},
		{name: "被讨厌的勇气 (岸見一郎  古賀史健).epub", embTitle: "被讨厌的勇气【公众号：书单严选】", embAuthors: []string{"岸見一郎", "古賀史健"}, embISBN: "9789861371955", publisher: "机械工业出版社", published: "2014-10-30T02:19:29.226000+00:00",
			title: "被讨厌的勇气", authors: []string{"岸見一郎", "古賀史健"}, isbns: []string{"9789861371955"}, titleFrom: "file name", authorFrom: "embedded"},
		{name: "鬼吹灯全集 (天下霸唱).epub", embTitle: "鬼吹灯全集", embAuthors: []string{"天下霸唱"}, publisher: "epub掌上书苑", published: "2011-04-17",
			title: "鬼吹灯全集", authors: []string{"天下霸唱"}, titleFrom: "file name", authorFrom: "embedded"},
	}
}

func (b realBook) metadata() *ebookmeta.Metadata {
	m := &ebookmeta.Metadata{Title: b.embTitle, Authors: b.embAuthors, Publisher: b.publisher, Published: b.published}
	if b.embISBN != "" {
		m.SetIdentifier("isbn", b.embISBN)
	}
	return m
}

// The query comes from the file name first and the embedded metadata second,
// and each part says where it came from.
func TestExtractOnTheRealBooks(t *testing.T) {
	for _, b := range realBooks() {
		x := Extract(b.name, b.metadata(), nil, Hint{})
		q := x.Query
		if q.Title != b.title || !reflect.DeepEqual(q.Authors, b.authors) || !reflect.DeepEqual(q.ISBNs, b.isbns) {
			t.Errorf("%s:\n got title=%q authors=%q isbns=%q\nwant title=%q authors=%q isbns=%q", b.name, q.Title, q.Authors, q.ISBNs, b.title, b.authors, b.isbns)
		}
		if x.TitleFrom != b.titleFrom || q.TitleGuessed != b.guessed {
			t.Errorf("%s: title from %q guessed=%v, want %q guessed=%v", b.name, x.TitleFrom, q.TitleGuessed, b.titleFrom, b.guessed)
		}
		if len(b.authors) > 0 && x.AuthorsFrom != b.authorFrom {
			t.Errorf("%s: authors from %q, want %q", b.name, x.AuthorsFrom, b.authorFrom)
		}
		// The file's own ISBN is confirmed; nothing the name gave is.
		if len(q.Unconfirmed) != 0 {
			t.Errorf("%s: unconfirmed = %v", b.name, q.Unconfirmed)
		}
		if len(x.ISBNFrom) != len(q.ISBNs) {
			t.Errorf("%s: %d ISBNs but %d sources named", b.name, len(q.ISBNs), len(x.ISBNFrom))
		}
	}
}

// What was found in the name and in the file is kept apart, for the page to
// show, and the other title is on offer.
func TestExtractKeepsBothSidesAndTheOtherTitle(t *testing.T) {
	b := realBooks()[2] // the polluted embedded title
	x := Extract(b.name, b.metadata(), nil, Hint{})
	if x.FileName.Title != "一读就上瘾的中国史" || x.Embedded.Title != b.embTitle {
		t.Fatalf("file=%q embedded=%q", x.FileName.Title, x.Embedded.Title)
	}
	if x.AltTitle != b.embTitle {
		t.Errorf("the embedded title is not on offer: %q", x.AltTitle)
	}
	if x.Embedded.Year != "2020" || x.Query.Year != "2020" {
		t.Errorf("year: embedded %q, query %q", x.Embedded.Year, x.Query.Year)
	}
	// Edition hints come from the file.
	d := Extract(realBooks()[1].name, realBooks()[1].metadata(), nil, Hint{})
	if d.Query.Publisher != "Penguin Publishing Group" || d.Query.Year != "2003" {
		t.Errorf("edition hints = %q %q", d.Query.Publisher, d.Query.Year)
	}
}

// A name that offers nothing leaves the file to speak, and the reverse.
func TestExtractFallsBackBothWays(t *testing.T) {
	good := &ebookmeta.Metadata{Title: "Real Title", Authors: []string{"Real Author"}}
	x := Extract("book.epub", good, nil, Hint{})
	if x.Query.Title != "Real Title" || x.TitleFrom != "embedded" || x.Query.TitleGuessed || !reflect.DeepEqual(x.Query.Authors, []string{"Real Author"}) {
		t.Errorf("a bare name: %+v from %q", x.Query, x.TitleFrom)
	}
	// An ISBN for a name, and nothing else anywhere: the ISBN is a hint.
	y := Extract("9787536692930.epub", &ebookmeta.Metadata{}, nil, Hint{})
	if y.Query.Title != "" || !reflect.DeepEqual(y.Query.ISBNs, []string{"9787536692930"}) || !reflect.DeepEqual(y.Query.Unconfirmed, y.Query.ISBNs) || y.ISBNFrom[0] != "file name" {
		t.Errorf("an ISBN-only name: %+v from %v", y.Query, y.ISBNFrom)
	}
	// A bare name that is a plain title is used when the file has none.
	z := Extract("Dune.epub", &ebookmeta.Metadata{}, nil, Hint{})
	if z.Query.Title != "Dune" || z.TitleFrom != "file name" || !z.Query.TitleGuessed {
		t.Errorf("a plain name: %+v from %q", z.Query, z.TitleFrom)
	}
	// Nothing anywhere is an empty query.
	if e := Extract("12345.epub", &ebookmeta.Metadata{Title: "Unknown"}, nil, Hint{}); !e.Query.Empty() {
		t.Errorf("a query from nothing: %+v", e.Query)
	}
}

// The ISBNs come in a fixed order and each says where it came from: the file's
// own, one a download recorded, the copyright page, the download's record, the
// file name. Only the file's own is confirmed.
func TestExtractOrdersAndLabelsISBNs(t *testing.T) {
	m := &ebookmeta.Metadata{Title: "Dune"}
	m.SetIdentifier("isbn", "9781101658055")
	m.SetIdentifier(SchemeZlibISBN, "9787536692930")
	x := Extract("Dune (Frank Herbert) 9780140328721.epub", m, []string{"9781593272203"}, Hint{ISBNs: []string{"9780441013593"}})
	wantISBN := []string{"9781101658055", "9787536692930", "9781593272203", "9780441013593", "9780140328721"}
	wantFrom := []string{"embedded", "recorded by a download", "copyright page", "download record", "file name"}
	if !reflect.DeepEqual(x.Query.ISBNs, wantISBN) || !reflect.DeepEqual(x.ISBNFrom, wantFrom) {
		t.Fatalf("ISBNs = %v from %v", x.Query.ISBNs, x.ISBNFrom)
	}
	if !reflect.DeepEqual(x.Query.Unconfirmed, wantISBN[1:]) {
		t.Errorf("unconfirmed = %v, want all but the file's own", x.Query.Unconfirmed)
	}
}

// The name and a recorded ISBN come from the same Z-Library record, so a title
// read from the name must not confirm that ISBN. Only a title the file itself
// carries can, and so can one the user types.
func TestAFileNameTitleDoesNotConfirmAHintISBN(t *testing.T) {
	const hint = "9780140328721"
	hit := Candidate{Title: "Matilda", EditionLevel: true, Sources: []string{"Fake"}, Identifiers: []ebookmeta.Identifier{{Scheme: "isbn", Value: hint}}}
	judge := func(x Extraction, cur *ebookmeta.Metadata) (float64, bool) {
		got := rankAndMerge(x.Query, []Candidate{hit}, defaultMinScore, 10)
		if len(got) != 1 {
			t.Fatalf("%d candidates", len(got))
		}
		for _, ch := range Plan(cur, got[0]) {
			if ch.Field == "identifier:isbn" {
				return got[0].Score, ch.DefaultSelected()
			}
		}
		return got[0].Score, false
	}

	// The file has no usable title; its name and a recorded ISBN agree.
	bare := &ebookmeta.Metadata{Title: "Unknown"}
	bare.SetIdentifier(SchemeZlibISBN, hint)
	x := Extract("Matilda (Roald Dahl).epub", bare, nil, Hint{})
	if !x.Query.TitleGuessed || x.Query.Title != "Matilda" {
		t.Fatalf("the title is not marked as guessed: %+v", x.Query)
	}
	if score, sel := judge(x, bare); score >= 1 || sel {
		t.Errorf("a file-name title confirmed a recorded ISBN: score=%v selected=%v", score, sel)
	}

	// The file carries the same title itself: two sources agree.
	agreeing := &ebookmeta.Metadata{Title: "Matilda"}
	agreeing.SetIdentifier(SchemeZlibISBN, hint)
	y := Extract("Matilda (Roald Dahl).epub", agreeing, nil, Hint{})
	if y.Query.TitleGuessed {
		t.Fatalf("an agreeing embedded title still counts as a guess: %+v", y.Query)
	}
	if score, sel := judge(y, agreeing); score != 1 || !sel {
		t.Errorf("the file's own title did not confirm it: score=%v selected=%v", score, sel)
	}

	// A title the user leaves alone keeps its mark; one they change is theirs.
	typed, err := ParseQueryEdit(x.Query, "Matilda", "", hint)
	if err != nil {
		t.Fatal(err)
	}
	if !typed.TitleGuessed {
		t.Errorf("a title the user left alone lost its guessed mark: %+v", typed)
	}
	typed, _ = ParseQueryEdit(x.Query, "Matilda Returns", "", hint)
	if typed.TitleGuessed {
		t.Errorf("a title the user typed is still called a guess")
	}
}

// Publisher and year pick between editions of one title, a little, and never
// outweigh an ISBN.
func TestEditionHintsBreakTiesBetweenEditions(t *testing.T) {
	q := Query{Title: "三体", Authors: []string{"刘慈欣"}, Publisher: "重庆出版社", Year: "2008"}
	other := Candidate{Title: "三体", Authors: []string{"刘慈欣"}, Publisher: "某出版社", Published: "2016-01", EditionLevel: true, Sources: []string{"A"},
		Identifiers: []ebookmeta.Identifier{{Scheme: "douban", Value: "1"}}}
	same := Candidate{Title: "三体", Authors: []string{"刘慈欣"}, Publisher: "重庆出版社", Published: "2008-01", EditionLevel: true, Sources: []string{"A"},
		Identifiers: []ebookmeta.Identifier{{Scheme: "douban", Value: "2"}}}
	got := rankAndMerge(q, []Candidate{other, same}, defaultMinScore, 10)
	if len(got) != 2 || got[0].Published != "2008-01" || got[0].Score < got[1].Score {
		t.Fatalf("the edition that agrees did not rank first: %+v", got)
	}
	if got[0].Score >= 1 {
		t.Errorf("a title match scored as an ISBN match: %v", got[0].Score)
	}
	// Either order in, the same edition comes first.
	if rev := rankAndMerge(q, []Candidate{same, other}, defaultMinScore, 10); rev[0].Published != "2008-01" {
		t.Errorf("the order of the input decided: %+v", rev)
	}
	// Without the hints the two score alike.
	plain := rankAndMerge(Query{Title: "三体", Authors: []string{"刘慈欣"}}, []Candidate{other, same}, defaultMinScore, 10)
	if plain[0].Score != plain[1].Score {
		t.Errorf("scores differ without hints: %v vs %v", plain[0].Score, plain[1].Score)
	}
	// Below the cap the hints raise the score, a little.
	loose := Query{Title: "三体全集", Publisher: "重庆出版社", Year: "2008"}
	a := Candidate{Title: "三体全集版", Publisher: "某出版社", Published: "2016-01", EditionLevel: true, Sources: []string{"A"}, Identifiers: []ebookmeta.Identifier{{Scheme: "douban", Value: "1"}}}
	b := Candidate{Title: "三体全集版", Publisher: "重庆出版社", Published: "2008-01", EditionLevel: true, Sources: []string{"A"}, Identifiers: []ebookmeta.Identifier{{Scheme: "douban", Value: "2"}}}
	r := rankAndMerge(loose, []Candidate{a, b}, defaultMinScore, 10)
	if len(r) != 2 || r[0].Published != "2008-01" || r[0].Score-r[1].Score < 0.06 || r[0].Score-r[1].Score > 0.08 {
		t.Errorf("the hints add %v, want about 0.07: %+v", r[0].Score-r[1].Score, r)
	}
	// An ISBN match is decided by the ISBN, whatever the hints say.
	withISBN := q
	withISBN.ISBNs = []string{"9787536692930"}
	hit := Candidate{Title: "三体", EditionLevel: true, Sources: []string{"A"}, Publisher: "别家", Published: "1999",
		Identifiers: []ebookmeta.Identifier{{Scheme: "isbn", Value: "9787536692930"}}}
	if r := rankAndMerge(withISBN, []Candidate{hit}, defaultMinScore, 10); len(r) != 1 || r[0].Score != 1 {
		t.Errorf("an ISBN match was changed by the hints: %+v", r)
	}
}

// Editing the search keeps what the extraction knew about the edition.
func TestParseQueryEditKeepsTheEditionHints(t *testing.T) {
	old := Query{Title: "三体", Publisher: "重庆出版社", Year: "2008", TitleGuessed: true}
	q, err := ParseQueryEdit(old, "三体", "刘慈欣", "")
	if err != nil {
		t.Fatal(err)
	}
	if q.Publisher != "重庆出版社" || q.Year != "2008" || !q.TitleGuessed {
		t.Errorf("an unchanged title lost what was known: %+v", q)
	}
}

// The page offers the other title: the file name's for the file's own and the
// reverse. Switching twice is a no-op, and the guessed mark follows the title.
func TestUseOtherTitleSwapsAndRecomputesTheGuess(t *testing.T) {
	b := realBooks()[2] // 一读就上瘾的中国史, with a polluted embedded title
	x := Extract(b.name, b.metadata(), nil, Hint{})
	if x.Query.Title != "一读就上瘾的中国史" || x.AltTitle != b.embTitle {
		t.Fatalf("start: %q / %q", x.Query.Title, x.AltTitle)
	}
	y := x.UseOtherTitle()
	if y.Query.Title != b.embTitle || y.TitleFrom != "embedded" || y.Query.TitleGuessed || y.AltTitle != "一读就上瘾的中国史" {
		t.Errorf("after one switch: title=%q from=%q guessed=%v alt=%q", y.Query.Title, y.TitleFrom, y.Query.TitleGuessed, y.AltTitle)
	}
	z := y.UseOtherTitle()
	if z.Query.Title != x.Query.Title || z.TitleFrom != x.TitleFrom || z.Query.TitleGuessed != x.Query.TitleGuessed || z.AltTitle != x.AltTitle {
		t.Errorf("two switches differ from the start: %+v vs %+v", z, x)
	}
	// Nothing to switch to: unchanged.
	d := Extract(realBooks()[1].name, realBooks()[1].metadata(), nil, Hint{})
	if d.AltTitle != "" || d.UseOtherTitle().Query.Title != "Dune" {
		t.Errorf("a book with one title changed: %+v", d.UseOtherTitle())
	}
	// A file with no usable title of its own: the name's is a guess, and going
	// back to it after an embedded title exists keeps it a guess only while the
	// file does not agree.
	n := Extract("亲密关系 (第8版).epub", &ebookmeta.Metadata{Title: "Unknown"}, nil, Hint{})
	if n.AltTitle != "" || !n.Query.TitleGuessed {
		t.Errorf("no embedded title: %+v", n.Query)
	}
	// The switch keeps the rest of the query: authors, ISBNs, edition hints.
	if y.Query.Publisher != x.Query.Publisher || !reflect.DeepEqual(y.Query.Authors, x.Query.Authors) || !reflect.DeepEqual(y.Query.ISBNs, x.Query.ISBNs) {
		t.Errorf("the switch changed more than the title: %+v vs %+v", y.Query, x.Query)
	}
}
