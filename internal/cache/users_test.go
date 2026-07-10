package cache

import (
	"testing"
)

func TestUpsertAndGetUser(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	u := User{
		ID:          "U123",
		WorkspaceID: "T1",
		Name:        "alice",
		DisplayName: "Alice Smith",
		Presence:    "active",
	}

	if err := db.UpsertUser(u); err != nil {
		t.Fatal(err)
	}

	got, err := db.GetUser("U123")
	if err != nil {
		t.Fatal(err)
	}
	if got.DisplayName != "Alice Smith" {
		t.Errorf("expected 'Alice Smith', got %q", got.DisplayName)
	}
	if got.Presence != "active" {
		t.Errorf("expected 'active', got %q", got.Presence)
	}
}

func TestUpsertUserPreservesIsExternal(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	u := User{
		ID:          "U_EXT",
		WorkspaceID: "T1",
		Name:        "ext.user",
		DisplayName: "External User",
		IsExternal:  true,
	}
	if err := db.UpsertUser(u); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetUser("U_EXT")
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsExternal {
		t.Errorf("IsExternal not persisted: got %+v", got)
	}
}

func TestUpsertUserDefaultsIsExternalFalse(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	u := User{ID: "U_INT", WorkspaceID: "T1", Name: "int.user", DisplayName: "Internal"}
	if err := db.UpsertUser(u); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetUser("U_INT")
	if err != nil {
		t.Fatal(err)
	}
	if got.IsExternal {
		t.Errorf("IsExternal should default to false; got %+v", got)
	}
}

func TestUpdatePresence(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	db.UpsertUser(User{ID: "U1", WorkspaceID: "T1", Name: "alice", Presence: "active"})

	if err := db.UpdatePresence("U1", "away"); err != nil {
		t.Fatal(err)
	}

	got, _ := db.GetUser("U1")
	if got.Presence != "away" {
		t.Errorf("expected 'away', got %q", got.Presence)
	}
}

func TestFilterCachedUsers(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	db.UpsertUser(User{ID: "U1", WorkspaceID: "T1", Name: "alice"})
	db.UpsertUser(User{ID: "U2", WorkspaceID: "T1", Name: "bob"})

	// Test basic filtering
	cached, err := db.FilterCachedUsers([]string{"U1", "U2", "U3"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cached["U1"]; !ok {
		t.Error("expected U1 to be cached")
	}
	if _, ok := cached["U2"]; !ok {
		t.Error("expected U2 to be cached")
	}
	if _, ok := cached["U3"]; ok {
		t.Error("expected U3 to not be cached")
	}

	// Test chunking (more than 999 elements)
	largeList := make([]string, 1200)
	for i := 0; i < 1200; i++ {
		largeList[i] = "U_LARGE_" + string(rune(i))
	}
	// Add some of them to cache
	db.UpsertUser(User{ID: largeList[10], WorkspaceID: "T1"})
	db.UpsertUser(User{ID: largeList[1050], WorkspaceID: "T1"})

	cachedLarge, err := db.FilterCachedUsers(largeList)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cachedLarge[largeList[10]]; !ok {
		t.Error("expected largeList[10] to be cached")
	}
	if _, ok := cachedLarge[largeList[1050]]; !ok {
		t.Error("expected largeList[1050] to be cached")
	}
	if len(cachedLarge) != 2 {
		t.Errorf("expected exactly 2 cached users, got %d", len(cachedLarge))
	}
}
