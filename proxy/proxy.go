package proxy

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/grafov/m3u8"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const START_KEY = "start"
const END_KEY = "end"

var segmentRegex = regexp.MustCompile(".*-(\\d+)\\..*")

var playlistContentTypes = map[string]struct{}{
	"application/x-mpegurl":         {},
	"application/vnd.apple.mpegurl": {},
}

func New(base *url.URL) func(http.ResponseWriter, *http.Request) {
	log.Trace().Str("base-url", base.String()).Msg("Creating new proxy")

	proxy := httputil.NewSingleHostReverseProxy(base)
	proxy.ModifyResponse = modifyResponse

	return func(w http.ResponseWriter, r *http.Request) {
		log.Debug().Msg("handling proxy")
		proxy.ServeHTTP(w, r)
	}
}

func modifyResponse(resp *http.Response) error {
	log := log.With().Str("requestId", uuid.New().String()).Logger()

	log.Debug().Msg("starting response modification")

	query := resp.Request.URL.Query()
	if !(query.Has(START_KEY) || query.Has(END_KEY)) {
		log.Debug().Msg("Query doesn't have start or end times, skipping further processing")
		return nil
	}

	if isPlaylist(resp) {
		log.Debug().Msg("request is a playlist")
		return handlePlaylist(log, resp)
	}

	log.Debug().Msg("request is not a playlist")
	return nil
}

func handlePlaylist(log zerolog.Logger, resp *http.Response) error {
	playlist, listType, err := m3u8.DecodeFrom(resp.Body, true)
	if err != nil {
		return fmt.Errorf("error decoding playlist: %w", err)
	}

	if listType == m3u8.MASTER {
		masterPlaylist, ok := playlist.(*m3u8.MasterPlaylist)
		if !ok {
			log.Error().Msg("Unable to coerce type into master playlist")
			return fmt.Errorf("unable to convert to master playlist")
		}

		log.Debug().Msg("Playlist is a master playlist")
		return handleMasterPlaylist(log, resp, masterPlaylist)
	}

	if listType == m3u8.MEDIA {
		mediaPlaylist, ok := playlist.(*m3u8.MediaPlaylist)
		if !ok {
			log.Error().Msg("Unable to coerce type into media playlist")
			return fmt.Errorf("unable to convert to media playlist")
		}

		return handleMediaPlaylist(log, resp, mediaPlaylist)
	}

	log.Warn().Msg("Got an unknown playlist type")
	return nil
}

func handleMasterPlaylist(log zerolog.Logger, r *http.Response, playlist *m3u8.MasterPlaylist) error {
	additionalArgs := url.Values{}
	query := r.Request.URL.Query()

	if startTime, found := query[START_KEY]; found {
		additionalArgs[START_KEY] = startTime
	}

	if endTime, found := query[END_KEY]; found {
		additionalArgs[END_KEY] = endTime
	}

	playlist.Args = additionalArgs.Encode()

	body := playlist.Encode()
	r.Body = io.NopCloser(body)
	r.ContentLength = int64(body.Len())
	r.Header.Set("Content-Length", strconv.Itoa(body.Len()))

	return nil
}

func handleMediaPlaylist(log zerolog.Logger, r *http.Response, incomingPlaylist *m3u8.MediaPlaylist) error {
	var start, end int64

	if err := int64QueryPart(r, START_KEY, &start); err != nil {
		return err
	}

	if err := int64QueryPart(r, END_KEY, &end); err != nil {
		return err
	}

	// The new playlist will be at most as large as the old playlist, so use that for the capacity count
	outgoing, err := m3u8.NewMediaPlaylist(incomingPlaylist.Count(), incomingPlaylist.Count())
	if err != nil {
		return fmt.Errorf("could not make new playlist: %w", err)
	}

	for _, segment := range incomingPlaylist.Segments {
		if segment == nil {
			continue
		}

		var shouldInclude bool
		if err := shouldIncludeSegment(segment, start, end, &shouldInclude); err != nil {
			return err
		}

		if shouldInclude {
			outgoing.AppendSegment(segment)
		}
	}

	// As a generic proxy, if we don't have an end specified leave the playlist open
	// as the backing service may still be writing segments, if it isn't signified as a VoD
	if end > 0 || incomingPlaylist.Closed {
		outgoing.Close()
	}

	// Copy params from the original
	outgoing.SeqNo = incomingPlaylist.SeqNo
	outgoing.DiscontinuitySeq = incomingPlaylist.DiscontinuitySeq

	body := outgoing.Encode()
	r.Body = io.NopCloser(body)
	r.ContentLength = int64(body.Len())
	r.Header.Set("Content-Length", strconv.Itoa(body.Len()))

	return nil
}

func isPlaylist(r *http.Response) bool {
	contentType := r.Header.Get("Content-Type")
	log.Debug().Str("content-type", contentType)

	_, found := playlistContentTypes[strings.ToLower(contentType)]

	return found
}

func parseInt64(raw string, val *int64) error {
	parsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		log.Err(err).Msg("Could not parse start time")
		return fmt.Errorf("unable to parse start time of %s: %w", raw, err)
	}

	*val = parsed
	return nil
}

func int64QueryPart(r *http.Response, key string, val *int64) error {
	if r.Request.URL.Query().Has(key) {
		raw := r.Request.URL.Query().Get(key)
		if err := parseInt64(raw, val); err != nil {
			return err
		}
	}

	return nil
}

func shouldIncludeSegment(segment *m3u8.MediaSegment, start, end int64, out *bool) error {
	if segment == nil {
		return fmt.Errorf("segment was nil")
	}
	parts := segmentRegex.FindStringSubmatch(segment.URI)
	if parts == nil {
		return fmt.Errorf("could not extract segment time: %s", segment.URI)
	}

	rawTimestamp := string(parts[1])
	var timestamp int64

	if err := parseInt64(rawTimestamp, &timestamp); err != nil {
		return err
	}

	if start > 0 {
		if timestamp < start {
			*out = false
			return nil
		}
	}

	if end > 0 {
		if timestamp > end {
			*out = false
			return nil
		}
	}

	*out = true
	return nil
}
