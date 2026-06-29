// Package memory manages the persistent project memory file.
// Memory is injected into Layer 0 (immutable prefix) and only changes
// on explicit /remember commands or Layer 5 Auto-Compaction.
//
// Five sections, stored as a markdown file:
//
//	## Tech Stack           — languages, frameworks, infrastructure
//	## Active Goals         — current tasks in progress
//	## Completed Tasks      — milestones achieved
//	## Key Architecture Map — component → responsibility mappings
//	## User Preferences     — explicit user rules and preferences
//
// TODO: consider migrating from fixed-field struct to a general key-value
// store for better extensibility (e.g. map[string]string).
package memory

import (
	"sync"
)

// File is the in-memory representation of the memory file.
type File struct {
	mu   sync.RWMutex
	path string

	TechStack      string
	ActiveGoals    string
	CompletedTasks string
	ArchMap        string
	Preferences    string

	fieldMap map[string]*string // initialized once
}

// sectionHeaders maps section names to their markdown headers.
var sectionHeaders = map[string]string{
	"TechStack":      "## Tech Stack",
	"ActiveGoals":    "## Active Goals",
	"CompletedTasks": "## Completed Tasks",
	"ArchMap":        "## Key Architecture Map",
	"Preferences":    "## User Preferences",
}

// sectionOrder is the canonical write order for Build/Save (map iteration is unordered).
var sectionOrder = []string{"TechStack", "ActiveGoals", "CompletedTasks", "ArchMap", "Preferences"}
