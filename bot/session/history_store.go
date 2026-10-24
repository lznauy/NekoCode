package session

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sync"

	"nekocode/bot/provider/types"
)

const historyHeaderSize = 64
const historyEntrySize = 32
const historyIndexMagic = 0x314958484f4b454e
const HistoryPageBytes = 1 << 20

// HistoryStore keeps only index metadata in memory. Contents remain in JSONL;
// the optional fixed-width index is safe to discard and rebuild.
type HistoryStore struct {
	mu  sync.Mutex
	dir string
}
type historyMeta struct{ size, mtime, count, owner int64 }
type historyEntry struct {
	offset, length, owner int64
	crc                   uint32
}
type HistoryPage struct {
	End      int
	Messages []types.Message
	Before   int // exclusive raw sequence cursor for the next older page
	HasMore  bool
	Total    int
}

func newHistoryStore(dir string) *HistoryStore { return &HistoryStore{dir: dir} }
func readHistoryMeta(f *os.File) (historyMeta, error) {
	var b [historyHeaderSize]byte
	if _, err := f.ReadAt(b[:], 0); err != nil {
		return historyMeta{}, err
	}
	if binary.LittleEndian.Uint64(b[:8]) != historyIndexMagic || crc32.ChecksumIEEE(b[:56]) != binary.LittleEndian.Uint32(b[56:60]) {
		return historyMeta{}, errors.New("invalid history index")
	}
	m := historyMeta{int64(binary.LittleEndian.Uint64(b[8:16])), int64(binary.LittleEndian.Uint64(b[16:24])), int64(binary.LittleEndian.Uint64(b[24:32])), int64(binary.LittleEndian.Uint64(b[32:40]))}
	if m.size < 0 || m.count < 0 || m.count > m.size || m.owner < 0 || m.owner > m.count {
		return historyMeta{}, errors.New("invalid history index bounds")
	}
	info, err := f.Stat()
	if err != nil {
		return m, err
	}
	if info.Size() < historyHeaderSize+m.count*historyEntrySize {
		return m, io.ErrUnexpectedEOF
	}
	return m, nil
}
func writeHistoryMeta(f *os.File, m historyMeta) error {
	var b [historyHeaderSize]byte
	binary.LittleEndian.PutUint64(b[:8], historyIndexMagic)
	for i, n := range []int64{m.size, m.mtime, m.count, m.owner} {
		binary.LittleEndian.PutUint64(b[8+i*8:16+i*8], uint64(n))
	}
	binary.LittleEndian.PutUint32(b[56:60], crc32.ChecksumIEEE(b[:56]))
	_, err := f.WriteAt(b[:], 0)
	return err
}
func readHistoryEntry(f *os.File, seq int64) (historyEntry, error) {
	if seq < 1 {
		return historyEntry{}, errors.New("invalid history sequence")
	}
	var b [historyEntrySize]byte
	_, err := f.ReadAt(b[:], historyHeaderSize+(seq-1)*historyEntrySize)
	e := historyEntry{int64(binary.LittleEndian.Uint64(b[:8])), int64(binary.LittleEndian.Uint64(b[8:16])), int64(binary.LittleEndian.Uint64(b[16:24])), binary.LittleEndian.Uint32(b[24:28])}
	if err == nil && (e.offset < 0 || e.length < 1 || e.length > 16<<20 || e.owner < 0 || e.owner > seq) {
		err = errors.New("invalid history index entry")
	}
	return e, err
}
func readHistoryRecord(f *os.File, e historyEntry, seq int64) (types.Message, error) {
	data := make([]byte, e.length)
	if _, err := f.ReadAt(data, e.offset); err != nil {
		return types.Message{}, err
	}
	if crc32.ChecksumIEEE(data) != e.crc {
		return types.Message{}, errors.New("history index checksum mismatch")
	}
	var r transcriptRecord
	if err := json.Unmarshal(data, &r); err != nil {
		return r.Message, err
	}
	if int64(r.Seq) != seq {
		return r.Message, errors.New("history sequence mismatch")
	}
	return r.Message, nil
}

// ensure builds/extends the index in a streaming pass. Only resume repairs a
// torn unconfirmed final record; ordinary page reads never truncate history.
func (s *HistoryStore) ensure(confirmed int, repair bool) (historyMeta, error) {
	path := filepath.Join(s.dir, transcriptFileName)
	source, err := os.Open(path)
	if os.IsNotExist(err) {
		if confirmed > 0 {
			return historyMeta{}, errors.New("transcript missing with confirmed messages")
		}
		return historyMeta{}, nil
	}
	if err != nil {
		return historyMeta{}, err
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return historyMeta{}, err
	}
	idx, err := os.OpenFile(filepath.Join(s.dir, "transcript.idx"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return historyMeta{}, err
	}
	defer idx.Close()
	m, indexErr := readHistoryMeta(idx)
	if indexErr == nil && m.size == info.Size() && m.mtime == info.ModTime().UnixNano() {
		if m.count < int64(confirmed) {
			return m, errors.New("transcript watermark exceeds records")
		}
		return m, nil
	}
	appendable := false
	if indexErr == nil && m.size < info.Size() && m.count > 0 {
		e, eerr := readHistoryEntry(idx, m.count)
		if eerr == nil && e.offset+e.length == m.size {
			_, eerr = readHistoryRecord(source, e, m.count)
			appendable = eerr == nil
		}
	}
	if !appendable {
		m = historyMeta{}
	}
	if err := idx.Truncate(historyHeaderSize + m.count*historyEntrySize); err != nil {
		return m, err
	}
	if _, err := source.Seek(m.size, io.SeekStart); err != nil {
		return m, err
	}
	scanner := bufio.NewScanner(source)
	scanner.Buffer(make([]byte, 64<<10), 16<<20)
	for scanner.Scan() {
		data := scanner.Bytes()
		var r transcriptRecord
		decodeErr := json.Unmarshal(data, &r)
		if decodeErr != nil || int64(r.Seq) != m.count+1 {
			if !repair || m.count < int64(confirmed) {
				return m, fmt.Errorf("invalid history record %d: %v", m.count+1, decodeErr)
			}
			if scanner.Scan() {
				return m, fmt.Errorf("corrupt history record before end: %d", m.count+1)
			}
			break
		}
		if r.Message.Role == "assistant" {
			m.owner = m.count + 1
		} else if r.Message.Role != "tool" {
			m.owner = 0
		}
		length := int64(len(data)) + 1
		// A valid last record without a newline is repaired on resume.
		if m.size+length > info.Size() {
			if !repair {
				return m, io.ErrUnexpectedEOF
			}
			if err := appendNewline(path); err != nil {
				return m, err
			}
		}
		var b [historyEntrySize]byte
		for i, n := range []int64{m.size, length, m.owner} {
			binary.LittleEndian.PutUint64(b[i*8:(i+1)*8], uint64(n))
		}
		// Scanner excludes the newline; records on disk always include it.
		checksum := crc32.Update(crc32.ChecksumIEEE(data), crc32.IEEETable, []byte{'\n'})
		binary.LittleEndian.PutUint32(b[24:28], checksum)
		if _, err := idx.WriteAt(b[:], historyHeaderSize+m.count*historyEntrySize); err != nil {
			return m, err
		}
		m.count++
		m.size += length
	}
	if err := scanner.Err(); err != nil {
		return m, err
	}
	if m.count < int64(confirmed) {
		return m, errors.New("transcript watermark exceeds records")
	}
	info, err = os.Stat(path)
	if err != nil {
		return m, err
	}
	if info.Size() != m.size {
		if !repair {
			return m, io.ErrUnexpectedEOF
		}
		if err := os.Truncate(path, m.size); err != nil {
			return m, err
		}
		info, err = os.Stat(path)
		if err != nil {
			return m, err
		}
	}
	m.mtime = info.ModTime().UnixNano()
	if err := writeHistoryMeta(idx, m); err != nil {
		return m, err
	}
	return m, nil
}

func (s *HistoryStore) Count(confirmed int, repair bool) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.ensure(confirmed, repair)
	return int(m.count), err
}
func (s *HistoryStore) ReadRange(first, last int) ([]types.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.ensure(last, false)
	if err != nil {
		return nil, err
	}
	if first < 1 || last < first-1 || int64(last) > m.count {
		return nil, errors.New("invalid history range")
	}
	if last < first {
		return nil, nil
	}
	source, err := os.Open(filepath.Join(s.dir, transcriptFileName))
	if err != nil {
		return nil, err
	}
	defer source.Close()
	idx, err := os.Open(filepath.Join(s.dir, "transcript.idx"))
	if err != nil {
		return nil, err
	}
	defer idx.Close()
	out := make([]types.Message, 0, last-first+1)
	for seq := first; seq <= last; seq++ {
		e, err := readHistoryEntry(idx, int64(seq))
		if err != nil {
			return nil, err
		}
		msg, err := readHistoryRecord(source, e, int64(seq))
		if err != nil {
			return nil, err
		}
		out = append(out, msg)
	}
	return out, nil
}

// Page reads older raw records up to a count and byte budget. If a page starts
// inside a tool batch, its owning assistant header is replayed for projection.
func (s *HistoryStore) Page(before, limit, maxBytes, through int) (HistoryPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit < 1 || limit > 1000 || maxBytes < 1 || before < 0 {
		return HistoryPage{}, errors.New("invalid history page")
	}
	m, err := s.ensure(through, false)
	if err != nil {
		return HistoryPage{}, err
	}
	total := min(int(m.count), through)
	if through < 0 {
		total = int(m.count)
	}
	end := total
	if before > 0 {
		end = min(end, before-1)
	}
	if end < 1 {
		return HistoryPage{Total: total}, nil
	}
	source, err := os.Open(filepath.Join(s.dir, transcriptFileName))
	if err != nil {
		return HistoryPage{}, err
	}
	defer source.Close()
	idx, err := os.Open(filepath.Join(s.dir, "transcript.idx"))
	if err != nil {
		return HistoryPage{}, err
	}
	defer idx.Close()
	first, bytes := end, 0
	for seq := end; seq >= 1 && end-seq < limit; seq-- {
		e, err := readHistoryEntry(idx, int64(seq))
		if err != nil {
			return HistoryPage{}, err
		}
		if bytes > 0 && bytes+int(e.length) > maxBytes {
			break
		}
		bytes += int(e.length)
		first = seq
	}
	page := HistoryPage{End: end, Before: first, HasMore: first > 1, Total: total}
	e, err := readHistoryEntry(idx, int64(first))
	if err != nil {
		return page, err
	}
	if e.owner > 0 && e.owner < int64(first) {
		owner, err := readHistoryEntry(idx, e.owner)
		if err != nil {
			return page, err
		}
		header, err := readHistoryRecord(source, owner, e.owner)
		if err != nil {
			return page, err
		}
		header.Content = ""
		page.Messages = append(page.Messages, header)
	}
	for seq := first; seq <= end; seq++ {
		e, err := readHistoryEntry(idx, int64(seq))
		if err != nil {
			return page, err
		}
		msg, err := readHistoryRecord(source, e, int64(seq))
		if err != nil {
			return page, err
		}
		page.Messages = append(page.Messages, msg)
	}
	return page, nil
}

// Append retries a previously written but uncommitted suffix without duplicating
// records. The context queue is acknowledged only after session.json commits.
func (s *HistoryStore) Append(base int, messages []types.Message) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return base, err
	}
	m, err := s.ensure(base, false)
	if err != nil {
		return base, err
	}
	if m.count < int64(base) || m.count > int64(base+len(messages)) {
		return base, errors.New("history changed outside pending append")
	}
	if int(m.count) > base {
		source, err := os.Open(filepath.Join(s.dir, transcriptFileName))
		if err != nil {
			return base, err
		}
		defer source.Close()
		idx, err := os.Open(filepath.Join(s.dir, "transcript.idx"))
		if err != nil {
			return base, err
		}
		defer idx.Close()
		for seq := base + 1; seq <= int(m.count); seq++ {
			e, err := readHistoryEntry(idx, int64(seq))
			if err != nil {
				return base, err
			}
			old, err := readHistoryRecord(source, e, int64(seq))
			if err != nil {
				return base, err
			}
			a, _ := json.Marshal(old)
			b, _ := json.Marshal(messages[seq-base-1])
			if string(a) != string(b) {
				return base, errors.New("uncommitted history differs from pending append")
			}
		}
	}
	return appendTranscriptFrom(s.dir, int(m.count), base, messages)
}

func (m *Manager) historyStore() *HistoryStore {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sess == nil || m.sess.ID == "" {
		return nil
	}
	if m.sess.history == nil {
		m.sess.history = newHistoryStore(filepath.Join(dir(), m.sess.ID))
	}
	return m.sess.history
}
func (m *Manager) HistoryPage(before, limit, maxBytes, through int) (HistoryPage, error) {
	s := m.historyStore()
	if s == nil {
		return HistoryPage{}, nil
	}
	return s.Page(before, limit, maxBytes, through)
}
func (m *Manager) HistoryRange(first, last int) ([]types.Message, error) {
	s := m.historyStore()
	if s == nil {
		return nil, nil
	}
	return s.ReadRange(first, last)
}

// WalkHistory streams chronological raw records without injecting page headers.
func (m *Manager) WalkHistory(through int, visit func(types.Message) error) error {
	if through < 1 {
		return nil
	}
	store := m.historyStore()
	if store == nil {
		return errors.New("no session history")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, err := store.ensure(through, false); err != nil {
		return err
	}
	f, err := os.Open(filepath.Join(store.dir, transcriptFileName))
	if err != nil {
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64<<10), 16<<20)
	for seq := 1; seq <= through; seq++ {
		if !scanner.Scan() {
			if scanner.Err() != nil {
				return scanner.Err()
			}
			return io.ErrUnexpectedEOF
		}
		var r transcriptRecord
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil {
			return err
		}
		if r.Seq != seq {
			return errors.New("history sequence mismatch")
		}
		if err := visit(r.Message); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) ExportHistory(through int, tail []types.Message, path string) (string, int, error) {
	if path == "" {
		path = DefaultExportPath
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", 0, err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".export-*.json")
	if err != nil {
		return "", 0, err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	defer f.Close()
	if _, err := f.WriteString("[\n"); err != nil {
		return "", 0, err
	}
	count := 0
	visit := func(msg types.Message) error {
		if count > 0 {
			if _, err := f.WriteString(",\n"); err != nil {
				return err
			}
		}
		data, err := json.MarshalIndent(msg, "  ", "  ")
		if err != nil {
			return err
		}
		if _, err = f.Write(data); err != nil {
			return err
		}
		count++
		return nil
	}
	if err := m.WalkHistory(through, visit); err != nil {
		return "", 0, err
	}
	for _, msg := range tail {
		if err := visit(msg); err != nil {
			return "", 0, err
		}
	}
	if _, err := f.WriteString("\n]\n"); err != nil {
		return "", 0, err
	}
	if err := f.Sync(); err != nil {
		return "", 0, err
	}
	if err := f.Close(); err != nil {
		return "", 0, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", 0, err
	}
	return path, count, nil
}
