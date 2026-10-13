package serverapp

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"lancast/internal/api"
	"lancast/internal/config"
	"lancast/internal/scan"
	"lancast/internal/store"
	"lancast/internal/tasks"
	"lancast/internal/update"
)

// taskDeps is what the jobs on the board need to be described and run.
type taskDeps struct {
	ctx        context.Context
	st         *store.Store
	scanner    *scan.Scanner
	settings   *config.SettingsStore
	updates    *update.Checker
	api        *api.Server
	artworkDir string
	log        *slog.Logger

	enrich, probe, markers, photos, places, roms, covers func()
}

/*
 * registerTasks puts every background job on the board behind Status ›
 * Scheduled tasks (docs/settings-plan.md, step 3), in the order a person would
 * read them: the scan first, then what follows it, then the timers.
 *
 * Run now calls exactly what the job's own trigger calls. Each pass already
 * coalesces concurrent requests, so pressing it during a run cannot start a
 * second one.
 */
func registerTasks(b *tasks.Board, d taskDeps) {
	b.Add(tasks.Spec{
		ID:          "scan",
		Name:        "Scan libraries",
		Description: "Finds new, changed and missing files in every library.",
		Trigger:     tasks.Interval,
		Every: func() time.Duration {
			return time.Duration(d.settings.Get().ScanIntervalHours) * time.Hour
		},
		// No Enabled: a scan can always be run on request. With no timer
		// set, Every is zero and the board reports no next run.
		Last: func() time.Time { return lastScan(d) },
		Busy: func() bool { return scanRunning(d) },
		Run:  func() { scanAll(d) },
	})
	afterScan := []struct {
		id, name, desc string
		run            func()
		enabled        func() bool
	}{
		{"probe", "Read media files", "Reads each new file's streams: codecs, resolution, languages, length.", d.probe, nil},
		{"enrich", "Fetch metadata", "Matches new titles and fetches their details and artwork.", d.enrich,
			func() bool { return d.settings.Get().AutoEnrich }},
		{"covers", "Album art", "Takes album art from the music files themselves.", d.covers, nil},
		{"markers", "Detect intros and credits", "Finds where intros and credits are in films and episodes, for the skip buttons.", d.markers,
			func() bool { return d.settings.Get().DetectMarkers }},
		{"photos", "Photo thumbnails", "Makes the small copies picture libraries browse with.", d.photos, nil},
		{"places", "Photo places", "Names the towns photographs were taken in, from their GPS.", d.places,
			func() bool { return d.settings.Get().PhotoPlaces }},
		{"roms", "Identify games", "Recognises retro game files from the ROM database.", d.roms, nil},
	}
	for _, j := range afterScan {
		b.Add(tasks.Spec{
			ID: j.id, Name: j.name, Description: j.desc,
			Trigger: tasks.AfterScan, Enabled: j.enabled, Run: j.run,
		})
	}
	b.Add(tasks.Spec{
		ID:          "guide",
		Name:        "Refresh TV guides",
		Description: "Fetches the programme guide for Live TV.",
		Trigger:     tasks.Interval,
		Every:       func() time.Duration { return guideEvery },
		Run:         func() { go refreshGuides(d.ctx, d.api, b) },
	})
	b.Add(tasks.Spec{
		ID:          "prune",
		Name:        "Maintenance",
		Description: "Clears old audit entries and unused artwork, and tidies the database.",
		Trigger:     tasks.Interval,
		Every:       func() time.Duration { return pruneEvery },
		Last: func() time.Time {
			last, _, err := d.st.LastPrune(d.ctx)
			if err != nil {
				return time.Time{}
			}
			return last
		},
		Run: func() { go trackedPrune(d.ctx, d.st, d.settings, d.artworkDir, d.log, b) },
	})
	b.Add(tasks.Spec{
		ID:          "update",
		Name:        "Check for updates",
		Description: "Asks GitHub whether a newer LANcast has been released.",
		Trigger:     tasks.Interval,
		Every:       func() time.Duration { return 24 * time.Hour },
		Enabled:     func() bool { return d.settings.Get().UpdateCheck },
		Last: func() time.Time {
			if at := d.updates.State().CheckedAt; at > 0 {
				return time.Unix(at, 0)
			}
			return time.Time{}
		},
		Run: func() {
			go func() {
				_ = b.Track("update", func() error {
					d.updates.Check(d.ctx)
					return nil
				})
			}()
		},
	})
}

// lastScan is the most recent scan of any library, from the database, so it
// survives a restart.
func lastScan(d taskDeps) time.Time {
	libs, err := d.st.ListLibraries(d.ctx)
	if err != nil {
		return time.Time{}
	}
	var last int64
	for _, l := range libs {
		if l.ScannedAt != nil && *l.ScannedAt > last {
			last = *l.ScannedAt
		}
	}
	if last == 0 {
		return time.Time{}
	}
	return time.Unix(last, 0)
}

func scanRunning(d taskDeps) bool {
	libs, err := d.st.ListLibraries(d.ctx)
	if err != nil {
		return false
	}
	for _, l := range libs {
		if d.scanner.Status(l.ID).State == scan.StateRunning {
			return true
		}
	}
	return false
}

// scanAll starts a scan of every library, skipping any already scanning.
func scanAll(d taskDeps) {
	libs, err := d.st.ListLibraries(d.ctx)
	if err != nil {
		d.log.Warn("scheduled tasks: list libraries", "error", err)
		return
	}
	for _, l := range libs {
		if _, err := d.scanner.Start(l); err != nil && !errors.Is(err, scan.ErrBusy) {
			d.log.Warn("scheduled tasks: start scan", "library", l.ID, "error", err)
		}
	}
}
