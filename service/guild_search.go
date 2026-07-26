package service

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/cockroachdb/errors"
)

const (
	searchPageSize      = 25
	maxSearchOffset     = 9975
	searchIndexCode     = 110000
	searchIndexMaxTries = 10
	searchPageDelay     = 350 * time.Millisecond
)

// guildMessagesSearchResult is the response of GET /guilds/{id}/messages/search.
// See https://discord.com/developers/docs/resources/message#search-guild-messages
type guildMessagesSearchResult struct {
	TotalResults int                       `json:"total_results"`
	Messages     [][]*discordgo.Message    `json:"messages"`
}

type searchIndexPending struct {
	Message    string  `json:"message"`
	Code       int     `json:"code"`
	RetryAfter float64 `json:"retry_after"`
}

// searchGuildMessages calls Discord's Search Guild Messages endpoint.
// discordgo v0.29 does not wrap this API yet.
func searchGuildMessages(s *discordgo.Session, guildID string, q url.Values) (*guildMessagesSearchResult, error) {
	endpoint := discordgo.EndpointGuild(guildID) + "/messages/search"
	uri := endpoint
	if len(q) > 0 {
		uri += "?" + q.Encode()
	}

	var lastErr error
	for try := 0; try < searchIndexMaxTries; try++ {
		body, err := s.RequestWithBucketID(http.MethodGet, uri, nil, endpoint)
		if err == nil {
			var result guildMessagesSearchResult
			if err := json.Unmarshal(body, &result); err != nil {
				return nil, errors.Wrap(err, "failed to decode search response")
			}
			return &result, nil
		}

		retryAfter, ok := searchIndexRetryAfter(err)
		if !ok {
			return nil, errors.Wrap(err, "guild message search failed")
		}
		lastErr = err
		if retryAfter <= 0 {
			retryAfter = 2
		}
		time.Sleep(time.Duration(retryAfter * float64(time.Second)))
	}
	return nil, errors.Wrap(lastErr, "guild message search index not ready")
}

func searchIndexRetryAfter(err error) (float64, bool) {
	var restErr *discordgo.RESTError
	if !errors.As(err, &restErr) || restErr.Response == nil {
		return 0, false
	}
	if restErr.Response.StatusCode != http.StatusAccepted {
		return 0, false
	}
	var pending searchIndexPending
	if json.Unmarshal(restErr.ResponseBody, &pending) != nil {
		return 2, true
	}
	if pending.Code != 0 && pending.Code != searchIndexCode {
		return 0, false
	}
	return pending.RetryAfter, true
}

func buildDetectionSearchQuery(channelID string, authorIDs []string, offset, limit int) url.Values {
	v := url.Values{}
	v.Set("content", detectionPrefix)
	v.Add("channel_id", channelID)
	for _, id := range authorIDs {
		if id != "" {
			v.Add("author_id", id)
		}
	}
	v.Set("include_nsfw", "true")
	v.Set("sort_by", "timestamp")
	v.Set("sort_order", "desc")
	if offset > 0 {
		v.Set("offset", strconv.Itoa(offset))
	}
	if limit > 0 {
		v.Set("limit", strconv.Itoa(limit))
	}
	return v
}

// pickSearchHit returns the matched message from a search hit group.
// Surrounding context is no longer returned by Discord; groups are typically a single message.
func pickSearchHit(group []*discordgo.Message) *discordgo.Message {
	for _, m := range group {
		if m == nil {
			continue
		}
		if _, ok := ParseDetectionReply(m.Content); ok {
			return m
		}
	}
	for _, m := range group {
		if m != nil {
			return m
		}
	}
	return nil
}
