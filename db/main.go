package db

type TagType = uint64

const (
	TagLatest TagType = 1 << iota
	TagSafe
	TagFinalized
	TagRemoved
)
