package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"kuzakizazi/internal/store"
)

// defaultReferralRewardCents is what every referrer earns when their
// referee's first paid order is confirmed. Configurable via the
// `referral_reward_cents` row in site_settings.
const defaultReferralRewardCents int64 = 20000 // KSh 200

// referralRewardCents returns the configured per-referral reward in cents.
func (a *API) referralRewardCents(ctx context.Context) int64 {
	settings, err := a.store.GetSettings(ctx)
	if err != nil {
		return defaultReferralRewardCents
	}
	if v, ok := settings["referral_reward_cents"]; ok && v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return defaultReferralRewardCents
}

// getMyCredit returns the signed-in user's store credit balance and ledger.
func (a *API) getMyCredit(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	balance, err := a.store.GetCreditBalance(r.Context(), uid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	txs, err := a.store.ListUserCreditTransactions(r.Context(), uid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"balanceCents": balance,
		"transactions": txs,
	})
}

// getMyReferrals returns the signed-in user's referral code, share URL,
// and the people they've referred so far.
func (a *API) getMyReferrals(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	code, err := a.store.GetOrCreateReferralCode(r.Context(), uid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	referees, err := a.store.ListReferees(r.Context(), uid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	reward := a.referralRewardCents(r.Context())
	share := strings.TrimRight(a.cfg.PublicBaseURL, "/") + "/register?ref=" + code
	writeJSON(w, http.StatusOK, map[string]any{
		"code":               code,
		"shareUrl":           share,
		"rewardCents":        reward,
		"referees":           referees,
		"totalReferrals":     len(referees),
		"rewardedCount":      countRewarded(referees),
	})
}

func countRewarded(rs []store.Referee) int {
	n := 0
	for _, r := range rs {
		if r.Rewarded {
			n++
		}
	}
	return n
}

// --- Admin rewards dashboard ---

func (a *API) adminRewardsOverview(w http.ResponseWriter, r *http.Request) {
	board, err := a.store.ReferralLeaderboard(r.Context(), 25)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	credits, err := a.store.RecentCreditTransactions(r.Context(), 25)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"rewardCents":       a.referralRewardCents(r.Context()),
		"topReferrers":      board,
		"recentCredit":      credits,
	})
}
