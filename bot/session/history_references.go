package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"nekocode/bot/extension/tool/runtime/refbridge"
)

// ReferencePage follows links in the existing disposable transcript index.
// Ordinary records are skipped without reading their JSON bodies. No additional
// reference store or full-session snapshot is retained in memory. A zero limit
// probes for older references without decoding a source record.
func (s *HistoryStore) ReferencePage(ctx context.Context, before, limit, maxBytes, through int) (refbridge.SourcePage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if before < 0 || limit < 0 || limit > refbridge.MaxPageRecords || maxBytes < 1 {
		return refbridge.SourcePage{}, errors.New("invalid reference page")
	}
	m, err := s.ensureContext(ctx, through, false)
	if err != nil {
		return refbridge.SourcePage{}, err
	}
	end := int(m.count)
	if through >= 0 {
		end = min(end, through)
	}
	if before > 0 {
		end = min(end, before-1)
	}
	if end < 1 {
		return refbridge.SourcePage{}, nil
	}
	idx, err := os.Open(filepath.Join(s.dir, "transcript.idx"))
	if err != nil {
		return refbridge.SourcePage{}, err
	}
	defer idx.Close()
	source, err := os.Open(filepath.Join(s.dir, transcriptFileName))
	if err != nil {
		return refbridge.SourcePage{}, err
	}
	defer source.Close()
	entry, err := readHistoryEntry(idx, int64(end))
	if err != nil {
		return refbridge.SourcePage{}, err
	}
	sequence := entry.reference
	out := refbridge.SourcePage{}
	for sequence > 0 {
		if err := ctx.Err(); err != nil {
			return refbridge.SourcePage{}, err
		}
		entry, err = readHistoryEntry(idx, sequence)
		if err != nil {
			return out, err
		}
		if entry.reference != sequence {
			return out, errors.New("invalid reference index link")
		}
		if len(out.Records) == limit || len(out.Records) > 0 && out.Bytes+int(entry.length) > maxBytes {
			break
		}
		msg, err := readHistoryRecord(source, entry, sequence)
		if err != nil {
			return out, err
		}
		if !refbridge.HasReference(msg, int(sequence)) {
			return out, errors.New("reference index disagrees with source")
		}
		out.Records = append(out.Records, refbridge.Record{Sequence: int(sequence), Message: msg})
		out.Bytes += int(entry.length)
		out.Before = int(sequence)
		if sequence == 1 {
			sequence = 0
			break
		}
		previous, err := readHistoryEntry(idx, sequence-1)
		if err != nil {
			return out, err
		}
		sequence = previous.reference
	}
	out.HasMore = sequence > 0
	return out, nil
}

func (m *Manager) ReferencePage(ctx context.Context, before, limit, maxBytes, through int) (refbridge.SourcePage, error) {
	store := m.historyStore()
	if store == nil {
		return refbridge.SourcePage{}, nil
	}
	return store.ReferencePage(ctx, before, limit, maxBytes, through)
}
