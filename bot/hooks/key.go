package hooks

const (
	KeyToolPrefix         = "tool:"          // + name
	KeyToolTaskResearcher = "task:researcher" // Turn
	KeyFileModified       = "file:modified"   // Flag
	KeyQuotaHard          = "quota:hard"     // Gauge
	KeyQuotaReads         = "quota:reads"    // Gauge
	KeyExploreScore       = "exploration:score" // Gauge
	KeyTasksAllDone       = "tasks:all_done" // Gauge
	KeyStepInput          = "step:input"      // Value
	KeyToolSig            = "tool:sig"        // Value
	KeyRespGarbled        = "resp:garbled"    // Counter (cross-turn)
	KeyRespChat           = "resp:chat"       // Turn
)
