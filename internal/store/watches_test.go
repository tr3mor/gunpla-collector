package store

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "w.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestWatches_CRUDAndStateReset(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	id, err := s.CreateWatch(ctx, Watch{Label: "Ka", Spec: `{"model":"x"}`, MaxPriceCents: 5000, OnRestock: true, Enabled: true}, "t0")
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.WatchByID(ctx, id)
	if err != nil || got.Label != "Ka" || got.MaxPriceCents != 5000 || !got.OnRestock || got.OnNew || !got.Enabled {
		t.Fatalf("watch = %+v, err %v", got, err)
	}

	st := WatchState{WatchID: id, LastAlertCents: 4000, WasBuyable: true, KnownSets: []int64{3, 4}}
	if err := s.SaveWatchState(ctx, st, "t1"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveWatchState(ctx, st, "t2"); err != nil { // upsert
		t.Fatal(err)
	}
	back, ok, err := s.WatchState(ctx, id)
	if err != nil || !ok || !reflect.DeepEqual(back, st) {
		t.Fatalf("state = %+v ok=%v err=%v, want %+v", back, ok, err, st)
	}

	got.Label = "renamed"
	if err := s.UpdateWatch(ctx, got, false, "t3"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.WatchState(ctx, id); !ok {
		t.Error("a rename dropped the alert state")
	}
	if err := s.UpdateWatch(ctx, got, true, "t4"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.WatchState(ctx, id); ok {
		t.Error("resetState kept the alert state")
	}

	if err := s.SaveWatchState(ctx, WatchState{WatchID: id}, "t5"); err != nil { // nil KnownSets
		t.Fatal(err)
	}
	if err := s.DeleteWatch(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.WatchState(ctx, id); ok {
		t.Error("deleting a watch left its state behind")
	}
	if err := s.DeleteWatch(ctx, id); !errors.Is(err, ErrWatchNotFound) {
		t.Errorf("second delete: %v, want ErrWatchNotFound", err)
	}
	if err := s.UpdateWatch(ctx, Watch{ID: id}, false, "t6"); !errors.Is(err, ErrWatchNotFound) {
		t.Errorf("update of a deleted watch: %v, want ErrWatchNotFound", err)
	}
}

func TestDismissedMatches_AreOrderIndependent(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if err := s.DismissMatch(ctx, 9, 4, "t"); err != nil {
		t.Fatal(err)
	}
	if err := s.DismissMatch(ctx, 4, 9, "t"); err != nil { // same pair again
		t.Fatal(err)
	}
	got, err := s.DismissedMatches(ctx)
	if err != nil || !reflect.DeepEqual(got, map[[2]int64]bool{{4, 9}: true}) {
		t.Fatalf("dismissed = %v, err %v", got, err)
	}
	if err := s.RestoreMatch(ctx, 9, 4); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.DismissedMatches(ctx); len(got) != 0 {
		t.Errorf("after restore: %v", got)
	}
}
