// Package zim is a small, read-only reader for ZIM archives (the format Kiwix
// uses for offline Wikipedia). It reads with random access via io.ReaderAt,
// so opening a multi-gigabyte archive costs only a few small reads.
//
// Supported: format versions 5 and 6, old (A/I/-) and new (C) namespace
// schemes, uncompressed, xz and zstd clusters, 32- and 64-bit blob offsets,
// redirects, and title-prefix search. Not supported: the embedded Xapian
// full-text index.
package zim

import (
	"bytes"
	"container/list"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

const (
	magicNumber = 72173914

	mimeRedirect   = 0xffff
	mimeLinkTarget = 0xfffe
	mimeDeleted    = 0xfffd

	noMainPage = 0xffffffff

	// Decompressed clusters kept in memory. Wikipedia clusters are usually
	// around 1–2 MB, so this bounds the cache at a few tens of MB.
	clusterCacheSize = 16
	// Redirect chains longer than this are treated as broken.
	maxRedirects = 16
)

var (
	ErrNotFound     = errors.New("zim: entry not found")
	ErrNotZim       = errors.New("zim: not a ZIM file")
	ErrUnsupported  = errors.New("zim: unsupported feature")
	ErrRedirectLoop = errors.New("zim: too many redirects")
)

// Header is the fixed 80-byte header at the start of every ZIM file.
type Header struct {
	MajorVersion  uint16
	MinorVersion  uint16
	UUID          [16]byte
	EntryCount    uint32
	ClusterCount  uint32
	PathPtrPos    uint64
	TitlePtrPos   uint64
	ClusterPtrPos uint64
	MimeListPos   uint64
	MainPage      uint32
	LayoutPage    uint32
	ChecksumPos   uint64
}

// Entry is one directory entry.
type Entry struct {
	Index     uint32
	Namespace byte
	Path      string // without namespace
	Title     string // falls back to Path when empty
	MimeType  string // empty for redirects

	IsRedirect    bool
	RedirectIndex uint32

	cluster uint32
	blob    uint32
}

// Archive is an open ZIM file. It is safe for concurrent use.
type Archive struct {
	r      io.ReaderAt
	closer io.Closer
	size   int64

	Header Header
	mimes  []string

	// NewNamespaces reports whether the archive uses the namespace scheme
	// introduced in format 6.1, where all user content lives in "C".
	NewNamespaces bool

	titleOnce sync.Once
	titles    titleIndex
	titleErr  error

	mu       sync.Mutex
	cache    map[uint32]*list.Element
	lru      *list.List
	zstdDec  *zstd.Decoder
	decodeMu sync.Mutex
}

type cachedCluster struct {
	n       uint32
	offsets []uint64
	data    []byte
}

// Open opens the ZIM file at path.
func Open(path string) (*Archive, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	a, err := New(f, st.Size())
	if err != nil {
		f.Close()
		return nil, err
	}
	a.closer = f
	return a, nil
}

// New reads a ZIM archive from r, which must be size bytes long.
func New(r io.ReaderAt, size int64) (*Archive, error) {
	a := &Archive{r: r, size: size, cache: map[uint32]*list.Element{}, lru: list.New()}

	var hb [80]byte
	if _, err := r.ReadAt(hb[:], 0); err != nil {
		return nil, fmt.Errorf("zim: reading header: %w", err)
	}
	le := binary.LittleEndian
	if le.Uint32(hb[0:]) != magicNumber {
		return nil, ErrNotZim
	}
	h := &a.Header
	h.MajorVersion = le.Uint16(hb[4:])
	h.MinorVersion = le.Uint16(hb[6:])
	copy(h.UUID[:], hb[8:24])
	h.EntryCount = le.Uint32(hb[24:])
	h.ClusterCount = le.Uint32(hb[28:])
	h.PathPtrPos = le.Uint64(hb[32:])
	h.TitlePtrPos = le.Uint64(hb[40:])
	h.ClusterPtrPos = le.Uint64(hb[48:])
	h.MimeListPos = le.Uint64(hb[56:])
	h.MainPage = le.Uint32(hb[64:])
	h.LayoutPage = le.Uint32(hb[68:])
	h.ChecksumPos = le.Uint64(hb[72:])

	if h.MajorVersion != 5 && h.MajorVersion != 6 {
		return nil, fmt.Errorf("%w: ZIM major version %d", ErrUnsupported, h.MajorVersion)
	}
	a.NewNamespaces = h.MajorVersion == 6 && h.MinorVersion >= 1

	for _, p := range []uint64{h.PathPtrPos, h.ClusterPtrPos, h.MimeListPos} {
		if p >= uint64(size) {
			return nil, fmt.Errorf("zim: header points past end of file (truncated copy?)")
		}
	}
	if h.PathPtrPos+uint64(h.EntryCount)*8 > uint64(size) || h.ClusterPtrPos+uint64(h.ClusterCount)*8 > uint64(size) {
		return nil, fmt.Errorf("zim: pointer lists extend past end of file (truncated copy?)")
	}

	if err := a.readMimeList(); err != nil {
		return nil, err
	}
	dec, err := zstd.NewReader(nil, zstd.WithDecoderConcurrency(1))
	if err != nil {
		return nil, err
	}
	a.zstdDec = dec
	return a, nil
}

// Close releases the underlying file.
func (a *Archive) Close() error {
	a.zstdDec.Close()
	if a.closer != nil {
		return a.closer.Close()
	}
	return nil
}

func (a *Archive) readMimeList() error {
	// The MIME list is a sequence of NUL-terminated strings ending with an
	// empty string. Read in chunks until the terminator appears.
	pos := int64(a.Header.MimeListPos)
	var buf []byte
	chunk := make([]byte, 4096)
	for len(buf) < 1<<20 {
		n, err := a.r.ReadAt(chunk, pos+int64(len(buf)))
		buf = append(buf, chunk[:n]...)
		if i := bytes.Index(buf, []byte{0, 0}); i >= 0 || (len(buf) > 0 && buf[0] == 0) {
			if buf[0] == 0 {
				return nil
			}
			for _, s := range strings.Split(string(buf[:i]), "\x00") {
				a.mimes = append(a.mimes, s)
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("zim: reading MIME list: %w", err)
		}
	}
	return errors.New("zim: MIME list too long")
}

// ContentNamespace is where articles live: "C" in new archives, "A" in old.
func (a *Archive) ContentNamespace() byte {
	if a.NewNamespaces {
		return 'C'
	}
	return 'A'
}

func (a *Archive) u64At(pos uint64) (uint64, error) {
	var b [8]byte
	if _, err := a.r.ReadAt(b[:], int64(pos)); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(b[:]), nil
}

func (a *Archive) u32At(pos uint64) (uint32, error) {
	var b [4]byte
	if _, err := a.r.ReadAt(b[:], int64(pos)); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(b[:]), nil
}

// EntryAt reads the directory entry with the given path-order index.
func (a *Archive) EntryAt(idx uint32) (*Entry, error) {
	if idx >= a.Header.EntryCount {
		return nil, ErrNotFound
	}
	pos, err := a.u64At(a.Header.PathPtrPos + uint64(idx)*8)
	if err != nil {
		return nil, err
	}
	// Fixed part is at most 16 bytes; paths and titles are short, so one
	// 1 KB read nearly always covers the whole entry.
	buf := make([]byte, 1024)
	n, err := a.r.ReadAt(buf, int64(pos))
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	buf = buf[:n]
	if len(buf) < 12 {
		return nil, fmt.Errorf("zim: short directory entry %d", idx)
	}
	le := binary.LittleEndian
	mime := le.Uint16(buf[0:])
	e := &Entry{Index: idx, Namespace: buf[3]}

	var strs []byte
	switch mime {
	case mimeRedirect:
		e.IsRedirect = true
		e.RedirectIndex = le.Uint32(buf[8:])
		strs = buf[12:]
	case mimeLinkTarget, mimeDeleted:
		strs = buf[8:]
	default:
		if len(buf) < 16 {
			return nil, fmt.Errorf("zim: short directory entry %d", idx)
		}
		if int(mime) < len(a.mimes) {
			e.MimeType = a.mimes[mime]
		}
		e.cluster = le.Uint32(buf[8:])
		e.blob = le.Uint32(buf[12:])
		strs = buf[16:]
	}

	path, rest, ok := cutNUL(strs)
	if !ok {
		return a.entryAtSlow(idx, pos, e, len(buf)-len(strs))
	}
	title, _, ok := cutNUL(rest)
	if !ok {
		return a.entryAtSlow(idx, pos, e, len(buf)-len(strs))
	}
	e.Path, e.Title = path, title
	if e.Title == "" {
		e.Title = e.Path
	}
	return e, nil
}

// entryAtSlow handles the rare entry whose strings don't fit in 1 KB.
func (a *Archive) entryAtSlow(idx uint32, pos uint64, e *Entry, fixed int) (*Entry, error) {
	buf := make([]byte, 64*1024)
	n, err := a.r.ReadAt(buf, int64(pos)+int64(fixed))
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	path, rest, ok := cutNUL(buf[:n])
	title, _, ok2 := cutNUL(rest)
	if !ok || !ok2 {
		return nil, fmt.Errorf("zim: malformed directory entry %d", idx)
	}
	e.Path, e.Title = path, title
	if e.Title == "" {
		e.Title = e.Path
	}
	return e, nil
}

func cutNUL(b []byte) (string, []byte, bool) {
	i := bytes.IndexByte(b, 0)
	if i < 0 {
		return "", nil, false
	}
	return string(b[:i]), b[i+1:], true
}

// Find looks up an entry by namespace and path (binary search over the
// path-ordered pointer list). Redirects are returned unresolved.
func (a *Archive) Find(ns byte, path string) (*Entry, error) {
	key := string(ns) + path
	var searchErr error
	n := int(a.Header.EntryCount)
	i := sort.Search(n, func(i int) bool {
		if searchErr != nil {
			return true
		}
		e, err := a.EntryAt(uint32(i))
		if err != nil {
			searchErr = err
			return true
		}
		return string(e.Namespace)+e.Path >= key
	})
	if searchErr != nil {
		return nil, searchErr
	}
	if i < n {
		e, err := a.EntryAt(uint32(i))
		if err != nil {
			return nil, err
		}
		if e.Namespace == ns && e.Path == path {
			return e, nil
		}
	}
	return nil, ErrNotFound
}

// Resolve follows redirects from e to a content entry.
func (a *Archive) Resolve(e *Entry) (*Entry, error) {
	for i := 0; e.IsRedirect; i++ {
		if i >= maxRedirects {
			return nil, ErrRedirectLoop
		}
		next, err := a.EntryAt(e.RedirectIndex)
		if err != nil {
			return nil, err
		}
		e = next
	}
	return e, nil
}

// MainPage returns the archive's main page entry, resolved.
func (a *Archive) MainPage() (*Entry, error) {
	if a.Header.MainPage != noMainPage {
		e, err := a.EntryAt(a.Header.MainPage)
		if err != nil {
			return nil, err
		}
		return a.Resolve(e)
	}
	if a.NewNamespaces {
		if e, err := a.Find('W', "mainPage"); err == nil {
			return a.Resolve(e)
		}
	}
	return nil, ErrNotFound
}

// Metadata returns an M-namespace value such as "Title", "Language",
// "Date", "Creator", "Publisher", "License" or "Description".
func (a *Archive) Metadata(name string) (string, error) {
	e, err := a.Find('M', name)
	if err != nil {
		return "", err
	}
	e, err = a.Resolve(e)
	if err != nil {
		return "", err
	}
	b, err := a.Content(e)
	return string(b), err
}

// Content returns the bytes of a (resolved) content entry.
func (a *Archive) Content(e *Entry) ([]byte, error) {
	if e.IsRedirect {
		return nil, errors.New("zim: Content called on a redirect")
	}
	if e.MimeType == "" {
		return nil, ErrNotFound
	}
	c, err := a.cluster(e.cluster)
	if err != nil {
		return nil, err
	}
	if int(e.blob)+1 >= len(c.offsets) {
		return nil, fmt.Errorf("zim: blob %d out of range in cluster %d", e.blob, e.cluster)
	}
	start, end := c.offsets[e.blob], c.offsets[e.blob+1]
	if start > end || end > uint64(len(c.data)) {
		return nil, fmt.Errorf("zim: corrupt blob offsets in cluster %d", e.cluster)
	}
	return c.data[start:end], nil
}

func (a *Archive) cluster(n uint32) (*cachedCluster, error) {
	a.mu.Lock()
	if el, ok := a.cache[n]; ok {
		a.lru.MoveToFront(el)
		a.mu.Unlock()
		return el.Value.(*cachedCluster), nil
	}
	a.mu.Unlock()

	c, err := a.loadCluster(n)
	if err != nil {
		return nil, err
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if el, ok := a.cache[n]; ok { // another goroutine got there first
		return el.Value.(*cachedCluster), nil
	}
	a.cache[n] = a.lru.PushFront(c)
	for a.lru.Len() > clusterCacheSize {
		old := a.lru.Back()
		a.lru.Remove(old)
		delete(a.cache, old.Value.(*cachedCluster).n)
	}
	return c, nil
}

func (a *Archive) loadCluster(n uint32) (*cachedCluster, error) {
	h := a.Header
	if n >= h.ClusterCount {
		return nil, fmt.Errorf("zim: cluster %d out of range", n)
	}
	start, err := a.u64At(h.ClusterPtrPos + uint64(n)*8)
	if err != nil {
		return nil, err
	}
	var end uint64
	if n+1 < h.ClusterCount {
		if end, err = a.u64At(h.ClusterPtrPos + uint64(n+1)*8); err != nil {
			return nil, err
		}
	} else if h.ChecksumPos > 0 && h.ChecksumPos <= uint64(a.size) {
		end = h.ChecksumPos
	} else {
		end = uint64(a.size)
	}
	if start >= end || end > uint64(a.size) {
		return nil, fmt.Errorf("zim: cluster %d has invalid bounds (truncated copy?)", n)
	}
	if end-start > 1<<30 {
		return nil, fmt.Errorf("zim: cluster %d is unreasonably large", n)
	}

	raw := make([]byte, end-start)
	if _, err := a.r.ReadAt(raw, int64(start)); err != nil {
		return nil, fmt.Errorf("zim: reading cluster %d: %w", n, err)
	}
	info := raw[0]
	extended := info&0x10 != 0
	var data []byte
	switch info & 0x0f {
	case 0, 1:
		data = raw[1:]
	case 4:
		xr, err := xz.NewReader(bytes.NewReader(raw[1:]))
		if err != nil {
			return nil, fmt.Errorf("zim: cluster %d xz: %w", n, err)
		}
		if data, err = io.ReadAll(xr); err != nil {
			return nil, fmt.Errorf("zim: cluster %d xz: %w", n, err)
		}
	case 5:
		a.decodeMu.Lock()
		data, err = a.zstdDec.DecodeAll(raw[1:], nil)
		a.decodeMu.Unlock()
		if err != nil {
			return nil, fmt.Errorf("zim: cluster %d zstd: %w", n, err)
		}
	default:
		return nil, fmt.Errorf("%w: cluster compression %d", ErrUnsupported, info&0x0f)
	}

	// Blob offset table: the first offset equals the table's own size.
	width := 4
	if extended {
		width = 8
	}
	if len(data) < width {
		return nil, fmt.Errorf("zim: cluster %d is empty", n)
	}
	readOff := func(i int) uint64 {
		if extended {
			return binary.LittleEndian.Uint64(data[i*8:])
		}
		return uint64(binary.LittleEndian.Uint32(data[i*4:]))
	}
	first := readOff(0)
	if first%uint64(width) != 0 || first > uint64(len(data)) || first == 0 {
		return nil, fmt.Errorf("zim: cluster %d has a corrupt offset table", n)
	}
	count := int(first) / width
	offsets := make([]uint64, count)
	for i := range offsets {
		offsets[i] = readOff(i)
	}
	return &cachedCluster{n: n, offsets: offsets, data: data}, nil
}

// titleIndex is a list of path-order entry indexes sorted by namespace+title.
// It is read either from disk (at base) or from memory (data).
type titleIndex struct {
	count int
	base  int64  // file offset of the first uint32, when reading from disk
	data  []byte // in-memory copy, when the listing is in a compressed cluster
}

func (a *Archive) titleAt(i int) (uint32, error) {
	t := &a.titles
	if t.data != nil {
		return binary.LittleEndian.Uint32(t.data[i*4:]), nil
	}
	return a.u32At(uint64(t.base) + uint64(i)*4)
}

// loadTitleIndex locates the title index. Older archives point to it from
// the header; format 6.2+ stores it as X/listing/titleOrdered/v1 (front
// articles only) or v0 (all entries).
func (a *Archive) loadTitleIndex() error {
	a.titleOnce.Do(func() {
		h := a.Header
		if h.TitlePtrPos != ^uint64(0) && h.TitlePtrPos+uint64(h.EntryCount)*4 <= uint64(a.size) {
			a.titles = titleIndex{count: int(h.EntryCount), base: int64(h.TitlePtrPos)}
			return
		}
		for _, name := range []string{"listing/titleOrdered/v1", "listing/titleOrdered/v0"} {
			e, err := a.Find('X', name)
			if err != nil {
				continue
			}
			if e, err = a.Resolve(e); err != nil {
				a.titleErr = err
				return
			}
			a.titles, a.titleErr = a.blobIndex(e)
			return
		}
		a.titleErr = fmt.Errorf("%w: archive has no title index", ErrUnsupported)
	})
	return a.titleErr
}

// blobIndex returns a titleIndex over entry e's blob. For uncompressed
// clusters it computes the blob's file offset so lookups read directly from
// disk; otherwise it falls back to the decompressed bytes.
func (a *Archive) blobIndex(e *Entry) (titleIndex, error) {
	h := a.Header
	if e.cluster < h.ClusterCount {
		start, err := a.u64At(h.ClusterPtrPos + uint64(e.cluster)*8)
		if err != nil {
			return titleIndex{}, err
		}
		var info [1]byte
		if _, err := a.r.ReadAt(info[:], int64(start)); err != nil {
			return titleIndex{}, err
		}
		if c := info[0] & 0x0f; c == 0 || c == 1 {
			width := uint64(4)
			if info[0]&0x10 != 0 {
				width = 8
			}
			readOff := func(i uint32) (uint64, error) {
				pos := start + 1 + uint64(i)*width
				if width == 8 {
					return a.u64At(pos)
				}
				v, err := a.u32At(pos)
				return uint64(v), err
			}
			bStart, err := readOff(e.blob)
			if err != nil {
				return titleIndex{}, err
			}
			bEnd, err := readOff(e.blob + 1)
			if err != nil {
				return titleIndex{}, err
			}
			if bEnd < bStart || start+1+bEnd > uint64(a.size) {
				return titleIndex{}, errors.New("zim: corrupt title index")
			}
			return titleIndex{count: int((bEnd - bStart) / 4), base: int64(start + 1 + bStart)}, nil
		}
	}
	b, err := a.Content(e)
	if err != nil {
		return titleIndex{}, err
	}
	return titleIndex{count: len(b) / 4, data: b}, nil
}

// TitleMatch is one search result.
type TitleMatch struct {
	Title string
	Path  string // target path in the content namespace (redirects resolved)
}

// SearchTitles returns up to limit content entries whose title starts with
// prefix (case-sensitive, as titles are stored). It binary-searches the
// title-ordered pointer list, then scans forward.
func (a *Archive) SearchTitles(prefix string, limit int) ([]TitleMatch, error) {
	if err := a.loadTitleIndex(); err != nil {
		return nil, err
	}
	ns := a.ContentNamespace()
	key := string(ns) + prefix
	n := a.titles.count

	titleEntry := func(i int) (*Entry, error) {
		idx, err := a.titleAt(i)
		if err != nil {
			return nil, err
		}
		return a.EntryAt(idx)
	}

	var searchErr error
	start := sort.Search(n, func(i int) bool {
		if searchErr != nil {
			return true
		}
		e, err := titleEntry(i)
		if err != nil {
			searchErr = err
			return true
		}
		return string(e.Namespace)+e.Title >= key
	})
	if searchErr != nil {
		return nil, searchErr
	}

	var out []TitleMatch
	seen := map[string]bool{}
	for i := start; i < n && len(out) < limit; i++ {
		e, err := titleEntry(i)
		if err != nil {
			return out, err
		}
		if e.Namespace != ns || !strings.HasPrefix(e.Title, prefix) {
			break
		}
		target, err := a.Resolve(e)
		if err != nil || target.Namespace != ns || !strings.HasPrefix(target.MimeType, "text/html") {
			continue
		}
		if seen[target.Path] {
			continue
		}
		seen[target.Path] = true
		out = append(out, TitleMatch{Title: e.Title, Path: target.Path})
	}
	return out, nil
}
