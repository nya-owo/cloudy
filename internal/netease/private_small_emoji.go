package netease

type SmallEmoji struct {
	Marker string
	URL    string
}

// DuoduoXixi uses the bracket names shared by Simple Cloud Music and Hydrogen
// Music, with the distinct CDN picture IDs from Hydrogen Music.
var DuoduoXixi = [...]SmallEmoji{
	{"[多多大笑]", privateStickerPicURL("109951163626285326")},
	{"[多多耍酷]", privateStickerPicURL("109951163626286808")},
	{"[多多比耶]", privateStickerPicURL("109951163626291112")},
	{"[多多大哭]", privateStickerPicURL("109951163626288209")},
	{"[多多瞌睡]", privateStickerPicURL("109951163626285332")},
	{"[多多难过]", privateStickerPicURL("109951163626282475")},
	{"[多多笑哭]", privateStickerPicURL("109951163626295026")},
	{"[多多可怜]", privateStickerPicURL("109951163626289680")},
	{"[多多无语]", privateStickerPicURL("109951163626291589")},
	{"[多多捂脸]", privateStickerPicURL("109951163626287335")},
	{"[多多亲吻]", privateStickerPicURL("109951163626285824")},
	{"[多多调皮]", privateStickerPicURL("109951163626288207")},
	{"[西西心动]", privateStickerPicURL("109951163626284860")},
	{"[西西发怒]", privateStickerPicURL("109951163626291586")},
	{"[西西惊讶]", privateStickerPicURL("109951163626290613")},
	{"[西西奸笑]", privateStickerPicURL("109951163626285329")},
	{"[西西晕了]", privateStickerPicURL("109951163626294527")},
	{"[西西机智]", privateStickerPicURL("109951163626295022")},
	{"[西西惊吓]", privateStickerPicURL("109951163626292571")},
	{"[西西流汗]", privateStickerPicURL("109951163626281959")},
	{"[西西呕吐]", privateStickerPicURL("109951163626287760")},
	{"[西西再见]", privateStickerPicURL("109951163626290116")},
	{"[西西疑问]", privateStickerPicURL("109951163626285827")},
}
