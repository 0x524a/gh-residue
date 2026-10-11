package probe

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/0x524a/gh-residue/internal/model"
)

const sha2 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func TestKnownIgnoresUnknownAndForeignSHAs(t *testing.T) {
	k := NewKnown([]model.Finding{
		{OldSHA: sha, Surface: model.SurfaceBaseObject, Location: "o/r", Status: model.Confirmed},
		{OldSHA: sha2, Surface: model.SurfaceBaseObject, Location: "o/r", Status: model.Unknown},
		{OldSHA: "cccccccccccccccccccccccccccccccccccccccc", Surface: model.SurfaceBaseObject, Location: "o/r", Status: model.Confirmed},
	}, []model.OldCommit{{SHA: sha}, {SHA: sha2}})
	if !k.Has(sha, model.SurfaceBaseObject, "o/r") {
		t.Error("confirmed must be known")
	}
	if k.Has(sha2, model.SurfaceBaseObject, "o/r") {
		t.Error("an unknown result must be retried, never skipped")
	}
	if len(k.Findings()) != 1 {
		t.Errorf("SHA not in current input must be dropped: %+v", k.Findings())
	}
	var nilK *Known
	if nilK.Has(sha, model.SurfaceBaseObject, "o/r") || nilK.SurfaceKnown(sha, model.SurfacePRRef) || nilK.Findings() != nil {
		t.Error("nil Known must be safe and empty")
	}
}

func TestBaseObjectResumeOnlyRechecksUnknown(t *testing.T) {
	var hits int32
	c, done := apiClient(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(200)
	})
	defer done()
	k := NewKnown([]model.Finding{
		{OldSHA: sha, Surface: model.SurfaceBaseObject, Location: "o/r", Status: model.NotFound},
		{OldSHA: sha2, Surface: model.SurfaceBaseObject, Location: "o/r", Status: model.Unknown},
	}, []model.OldCommit{{SHA: sha}, {SHA: sha2}})
	res := BaseObject{Client: c}.WithKnown(k).Run(context.Background(), "o/r", []model.OldCommit{{SHA: sha}, {SHA: sha2}})
	if hits != 1 || len(res.Findings) != 1 || res.Findings[0].OldSHA != sha2 || res.Findings[0].Status != model.Confirmed {
		t.Fatalf("hits=%d %+v", hits, res.Findings)
	}
}

func TestForksResumeSkipsKnownForkSHAPairs(t *testing.T) {
	var commitHits int32
	inner := forksServer(3, "b/hot")
	c, done := apiClient(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/commits/"+sha) {
			atomic.AddInt32(&commitHits, 1)
		}
		inner(w, r)
	})
	defer done()
	k := NewKnown([]model.Finding{
		{OldSHA: sha, Surface: model.SurfaceForkObject, Location: "b/hot", Status: model.Confirmed},
		{OldSHA: sha, Surface: model.SurfaceForkObject, Location: "c/mid", Status: model.NotFound},
		{OldSHA: sha, Surface: model.SurfaceForkObject, Location: "a/old", Status: model.Unknown},
	}, []model.OldCommit{{SHA: sha}})
	res := Forks{Client: c}.WithKnown(k).Run(context.Background(), "o/r", []model.OldCommit{{SHA: sha}})
	if commitHits != 1 || len(res.Findings) != 1 || res.Findings[0].Location != "a/old" {
		t.Fatalf("hits=%d %+v", commitHits, res.Findings)
	}
	if res.ForksChecked != 3 || !res.Surfaces[0].Complete {
		t.Errorf("coverage must still count carried forks: %+v", res)
	}
}

func TestMirrorResumeSkipsCloneWhenAllKnown(t *testing.T) {
	root := t.TempDir()
	var known []model.Finding
	for _, s := range refSurfaces {
		known = append(known, model.Finding{OldSHA: sha, Surface: s, Location: "*", Status: model.NotFound})
	}
	k := NewKnown(known, []model.OldCommit{{SHA: sha}})
	// CloneURL is bogus: any clone attempt would make the result Failed.
	res := Mirror{CloneURL: root + "/does-not-exist.git", TempRoot: root}.WithKnown(k).
		Run(context.Background(), "o/r", []model.OldCommit{{SHA: sha}})
	if res.Failed || len(res.Findings) != 0 || len(res.Surfaces) != 3 {
		t.Fatalf("%+v", res)
	}
	for _, s := range res.Surfaces {
		if !s.Complete {
			t.Errorf("%+v", s)
		}
	}
}

func TestMirrorResumeRechecksWholeSHAWhenAnySurfaceUnknown(t *testing.T) {
	bare, b, _, _, _ := fixture(t)
	root := t.TempDir()
	k := NewKnown([]model.Finding{
		{OldSHA: b, Surface: model.SurfaceBranch, Location: "*", Status: model.NotFound},
		{OldSHA: b, Surface: model.SurfacePRRef, Location: "*", Status: model.Unknown},
	}, []model.OldCommit{{SHA: b}})
	res := Mirror{CloneURL: bare, TempRoot: root}.WithKnown(k).Run(context.Background(), "o/r", []model.OldCommit{{SHA: b}})
	if res.Failed || len(find(res.Findings, b, model.SurfacePRRef)) == 0 || len(find(res.Findings, b, model.SurfaceBranch)) == 0 {
		t.Fatalf("all three surfaces must be re-evaluated: %+v", res.Findings)
	}
}
