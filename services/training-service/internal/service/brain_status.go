package service

// PublicBrainStatus deliberately excludes director reasons, parameters and private evidence.
type PublicBrainStatus struct {
	Phase            string `json:"phase"`
	CurrentChallenge int    `json:"current_challenge"`
	Status           string `json:"status"`
	Reason           string `json:"reason"`
	CleanupStatus    string `json:"cleanup_status"`
	AbilityObserved  bool   `json:"ability_observed"`
}
