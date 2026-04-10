package zlib

type Extension string

const (
	ExtTXT  Extension = "TXT"
	ExtPDF  Extension = "PDF"
	ExtFB2  Extension = "FB2"
	ExtEPUB Extension = "EPUB"
	ExtLIT  Extension = "LIT"
	ExtMOBI Extension = "MOBI"
	ExtRTF  Extension = "RTF"
	ExtDJV  Extension = "DJV"
	ExtDJVU Extension = "DJVU"
	ExtAZW  Extension = "AZW"
	ExtAZW3 Extension = "AZW3"
)

func (e Extension) String() string { return string(e) }

type OrderOption string

const (
	OrderPopular OrderOption = "popular"
	OrderNewest  OrderOption = "date_created"
	OrderRecent  OrderOption = "date_updated"
)

func (o OrderOption) String() string { return string(o) }

type Language string

const (
	LangEnglish    Language = "english"
	LangChinese    Language = "chinese"
	LangRussian    Language = "russian"
	LangFrench     Language = "french"
	LangGerman     Language = "german"
	LangSpanish    Language = "spanish"
	LangJapanese   Language = "japanese"
	LangKorean     Language = "korean"
	LangItalian    Language = "italian"
	LangPortuguese Language = "portuguese"
	LangArabic     Language = "arabic"
	LangDutch      Language = "dutch"
	LangPolish     Language = "polish"
	LangTurkish    Language = "turkish"
	LangHindi      Language = "hindi"
)
