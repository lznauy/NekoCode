package session

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"nekocode/bot/provider/types"
	"nekocode/protocol"
)

// SubagentInfo is a bounded read model. It never carries active model context.
type SubagentInfo struct {
	Isolation        string           `json:"isolation,omitempty"`
	ID               string           `json:"id"`
	ParentSessionID  string           `json:"parent_session_id"`
	Title            string           `json:"title,omitempty"`
	Profile          string           `json:"profile"`
	Skills           []string         `json:"skills,omitempty"`
	Prompt           string           `json:"prompt"`
	Status           string           `json:"status"`
	RunCount         int              `json:"run_count"`
	Result           string           `json:"result,omitempty"`
	CreatedAt        int64            `json:"created_at"`
	UpdatedAt        int64            `json:"updated_at"`
	UpdatedAtNano    int64            `json:"-"` // actual committed snapshot mtime, not second-resolution UI time
	TranscriptSeq    int              `json:"transcript_seq"`
	Error            string           `json:"error,omitempty"`
	CurrentAttemptID string           `json:"current_attempt_id,omitempty"`
	LatestAttempt    *SubagentAttempt `json:"latest_attempt,omitempty"`
}

type SubagentAttemptPage struct {
	Attempts []SubagentAttempt
	Before   int
	End      int
	Total    int
	HasMore  bool
}

type subagentProjection struct {
	SourceSize int64        `json:"source_size"`
	SourceTime int64        `json:"source_time"`
	Info       SubagentInfo `json:"info"`
}

func infoFromSnapshot(s *Snapshot) SubagentInfo {
	m := s.Subagent
	var latest *SubagentAttempt
	if attempt := m.CurrentAttempt(); attempt != nil {
		copy := *attempt
		copy.Prompt = protocol.SubagentTextPreview(copy.Prompt, 1024)
		copy.Result = protocol.SubagentTextPreview(copy.Result, 4096)
		latest = &copy
	}
	return SubagentInfo{Isolation: m.Isolation, ID: s.ID, ParentSessionID: m.ParentSessionID, Title: m.Title, Profile: m.Profile, Skills: append([]string(nil), m.Skills...), Prompt: protocol.SubagentTextPreview(m.Prompt, 1024), Status: m.Status, RunCount: m.RunCount, Result: protocol.SubagentTextPreview(m.Result, 4096), CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt, TranscriptSeq: s.TranscriptSeq, CurrentAttemptID: m.CurrentAttemptID, LatestAttempt: latest}
}

// Projection failure does not invalidate a committed context. Readers detect
// a missing/stale projection using the source file identity and rebuild it.
func (s *SubagentSession) writeProjection() {
	d := filepath.Join(s.root, s.snapshot.ID)
	stat, err := os.Stat(filepath.Join(d, "session.json"))
	if err != nil {
		return
	}
	data, err := json.Marshal(subagentProjection{SourceSize: stat.Size(), SourceTime: stat.ModTime().UnixNano(), Info: infoFromSnapshot(s.snapshot)})
	if err == nil {
		_ = writeAtomic(filepath.Join(d, "observation.json"), data, 0600)
	}
}

func readSubagentInfo(root, parentID, id string) (SubagentInfo, error) {
	d := filepath.Join(root, id)
	stat, err := os.Stat(filepath.Join(d, "session.json"))
	if err != nil {
		return SubagentInfo{}, err
	}
	var projection subagentProjection
	data, err := os.ReadFile(filepath.Join(d, "observation.json"))
	if err == nil && json.Unmarshal(data, &projection) == nil && projection.SourceSize == stat.Size() && projection.SourceTime == stat.ModTime().UnixNano() && projection.Info.ID == id && projection.Info.ParentSessionID == parentID {
		projection.Info.UpdatedAtNano = stat.ModTime().UnixNano()
		return observeOwnership(d, projection.Info)
	}
	// Decode only required fields on cache miss; active context is discarded.
	f, err := os.Open(filepath.Join(d, "session.json"))
	if err != nil {
		return SubagentInfo{}, err
	}
	defer f.Close()
	var header struct {
		ID            string            `json:"id"`
		FormatVersion int               `json:"format_version"`
		CreatedAt     int64             `json:"created_at"`
		UpdatedAt     int64             `json:"updated_at"`
		TranscriptSeq int               `json:"transcript_seq"`
		Subagent      *SubagentMetadata `json:"subagent"`
	}
	if err := json.NewDecoder(f).Decode(&header); err != nil {
		return SubagentInfo{}, err
	}
	if header.ID != id || header.Subagent == nil || header.Subagent.ParentSessionID != parentID || (header.FormatVersion < 2 || header.FormatVersion > currentSnapshotVersion) || header.TranscriptSeq < 0 {
		return SubagentInfo{}, fmt.Errorf("invalid subagent snapshot")
	}
	info := infoFromSnapshot(&Snapshot{ID: id, CreatedAt: header.CreatedAt, UpdatedAt: header.UpdatedAt, TranscriptSeq: header.TranscriptSeq, Subagent: header.Subagent})
	info.UpdatedAtNano = stat.ModTime().UnixNano()
	return observeOwnership(d, info)
}

func observeOwnership(d string, info SubagentInfo) (SubagentInfo, error) {
	if info.Status != "running" {
		return info, nil
	}
	f, err := os.OpenFile(filepath.Join(d, ".active.lock"), os.O_RDWR, 0600)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return info, err
	}
	acquired := f == nil
	if f != nil {
		defer f.Close()
		acquired, err = trySubagentLock(f)
		if err != nil {
			return info, err
		}
	}
	if acquired {
		info.Status = "interrupted"
		if info.LatestAttempt != nil && info.LatestAttempt.Status == "running" {
			info.LatestAttempt.Status = "interrupted"
		}
	}
	return info, nil
}

// ListSubagents returns stable creation order. Corrupt tasks remain visible
// as unavailable rows instead of hiding every other task in the conversation.
func ListSubagents(parentID string) ([]SubagentInfo, error) {
	root, err := subagentRoot(parentID)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return []SubagentInfo{}, nil
	}
	if err != nil {
		return nil, err
	}
	infos := make([]SubagentInfo, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		info, err := readSubagentInfo(root, parentID, entry.Name())
		if errors.Is(err, os.ErrNotExist) {
			continue
		} // reserved but not yet committed
		if err != nil {
			info = SubagentInfo{ID: entry.Name(), ParentSessionID: parentID, Status: "unavailable", Error: err.Error()}
		}
		infos = append(infos, info)
	}
	sort.Slice(infos, func(i, j int) bool {
		if infos[i].CreatedAt != infos[j].CreatedAt {
			return infos[i].CreatedAt < infos[j].CreatedAt
		}
		return infos[i].ID < infos[j].ID
	})
	return infos, nil
}

// SubagentAttempts returns known attempts newest-page-last without loading
// the task's model context or transcript. Legacy runs are not reconstructed.
func SubagentAttempts(parentID, id string, before, limit int) (SubagentAttemptPage, error) {
	if before < 0 || limit < 1 || limit > 1000 {
		return SubagentAttemptPage{}, fmt.Errorf("invalid subagent attempt cursor or limit")
	}
	root, err := subagentRoot(parentID)
	if err != nil {
		return SubagentAttemptPage{}, err
	}
	if err := validateID(id); err != nil {
		return SubagentAttemptPage{}, err
	}
	info, err := readSubagentInfo(root, parentID, id)
	if err != nil {
		return SubagentAttemptPage{}, err
	}
	file, err := os.Open(filepath.Join(root, id, "session.json"))
	if err != nil {
		return SubagentAttemptPage{}, err
	}
	defer file.Close()
	var header struct {
		Subagent *SubagentMetadata `json:"subagent"`
	}
	if err := json.NewDecoder(file).Decode(&header); err != nil {
		return SubagentAttemptPage{}, err
	}
	if header.Subagent == nil || header.Subagent.ParentSessionID != parentID {
		return SubagentAttemptPage{}, fmt.Errorf("invalid subagent snapshot")
	}
	eligible := make([]SubagentAttempt, 0, len(header.Subagent.Attempts))
	for _, attempt := range header.Subagent.Attempts {
		if before > 0 && attempt.Seq >= before {
			continue
		}
		attempt.Prompt = protocol.SubagentTextPreview(attempt.Prompt, 1024)
		attempt.Result = protocol.SubagentTextPreview(attempt.Result, 4096)
		if info.Status == "interrupted" && info.LatestAttempt != nil && attempt.ID == info.LatestAttempt.ID && attempt.Status == "running" {
			attempt.Status = "interrupted"
		}
		eligible = append(eligible, attempt)
	}
	first := max(0, len(eligible)-limit)
	page := SubagentAttemptPage{Attempts: eligible[first:], Total: len(header.Subagent.Attempts), HasMore: first > 0}
	if len(page.Attempts) > 0 {
		page.Before = page.Attempts[0].Seq
		page.End = page.Attempts[len(page.Attempts)-1].Seq
	}
	return page, nil
}

// SubagentHistory reads only the committed prefix. It never repairs history
// or writes an index while an executor may be appending to the transcript.
func SubagentHistory(parentID, id string, before, limit int) (HistoryPage, error) {
	if before < 0 || limit < 1 || limit > 1000 {
		return HistoryPage{}, fmt.Errorf("invalid history cursor or limit")
	}
	root, err := subagentRoot(parentID)
	if err != nil {
		return HistoryPage{}, err
	}
	if err := validateID(id); err != nil {
		return HistoryPage{}, err
	}
	info, err := readSubagentInfo(root, parentID, id)
	if err != nil {
		return HistoryPage{}, err
	}
	end := info.TranscriptSeq
	if before > 0 {
		end = min(end, before-1)
	}
	if end == 0 {
		return HistoryPage{Total: info.TranscriptSeq}, nil
	}
	d := filepath.Join(root, id)
	f, err := os.Open(filepath.Join(d, transcriptFileName))
	if err != nil {
		return HistoryPage{}, err
	}
	defer f.Close()
	first := max(1, end-limit+1)
	sequence := 0
	var owner *types.Message
	// The writer's index can lag the last committed batch. Seek to the nearest
	// indexed record and scan forward through the watermark, never the live tail.
	if idx, e := os.Open(filepath.Join(d, "transcript.idx")); e == nil {
		if meta, e := readHistoryMeta(idx); e == nil && meta.count > 0 {
			anchor := min(int64(first), meta.count)
			if entry, e := readHistoryEntry(idx, anchor); e == nil {
				if _, e := readHistoryRecord(f, entry, anchor); e == nil {
					if _, e = f.Seek(entry.offset, 0); e == nil {
						sequence = int(anchor) - 1
					}
					if entry.owner > 0 && entry.owner < anchor {
						if oe, e := readHistoryEntry(idx, entry.owner); e == nil {
							if msg, e := readHistoryRecord(f, oe, entry.owner); e == nil {
								msg.Content = ""
								owner = &msg
							}
						}
					}
				}
			}
		}
		idx.Close()
	}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64<<10), 16<<20)
	page := HistoryPage{End: end, Total: info.TranscriptSeq}
	bytes := 0
	var sizes []int
	for sequence < end && scanner.Scan() {
		var record transcriptRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil || record.Seq != sequence+1 {
			return HistoryPage{}, fmt.Errorf("invalid confirmed subagent record %d", sequence+1)
		}
		sequence++
		if sequence < first {
			if record.Message.Role == "assistant" {
				msg := record.Message
				msg.Content = ""
				owner = &msg
			} else if record.Message.Role != "tool" {
				owner = nil
			}
			continue
		}
		page.Messages = append(page.Messages, record.Message)
		bytes += len(scanner.Bytes())
		sizes = append(sizes, len(scanner.Bytes()))
		if page.Before == 0 {
			page.Before = sequence
		}
		for bytes > HistoryPageBytes && len(page.Messages) > 1 {
			bytes -= sizes[0]
			sizes = sizes[1:]
			if page.Messages[0].Role == "assistant" {
				msg := page.Messages[0]
				msg.Content = ""
				owner = &msg
			}
			page.Messages[0] = types.Message{}
			page.Messages = page.Messages[1:]
			page.Before++
		}
	}
	if err := scanner.Err(); err != nil {
		return HistoryPage{}, err
	}
	if sequence != end {
		return HistoryPage{}, fmt.Errorf("transcript watermark exceeds records")
	}
	if len(page.Messages) > 0 && page.Messages[0].Role == "tool" && owner != nil {
		page.Messages = append([]types.Message{*owner}, page.Messages...)
	}
	page.HasMore = page.Before > 1
	return page, nil
}
