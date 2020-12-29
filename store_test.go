package sqlitestore

import (
	"database/sql"
	"io/ioutil"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSession(t *testing.T) {
	tmpdir, err := ioutil.TempDir("", "store-test")
	require.NoError(t, err)
	path := filepath.Join(tmpdir, "test.db")
	db, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	defer os.RemoveAll(tmpdir)

	store, err := NewStore(db, securecookie.GenerateRandomKey(32))
	require.NoError(t, err)

	r := httptest.NewRequest("GET", "/", nil)
	sess, err := store.New(r, "test")
	assert.NoError(t, err)
	assert.True(t, sess.IsNew)
	w := httptest.NewRecorder()
	assert.NoError(t, sess.Save(r, w))
	t.Logf(w.Header().Get("Set-Cookie"))
}

func TestSessionRenewal(t *testing.T) {
	tmpdir, err := ioutil.TempDir("", "store-test")
	require.NoError(t, err)
	path := filepath.Join(tmpdir, "test.db")
	db, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	defer os.RemoveAll(tmpdir)

	store, err := NewStore(db, securecookie.GenerateRandomKey(32))
	require.NoError(t, err)

	r := httptest.NewRequest("GET", "/", nil)
	sess, err := store.New(r, "test")
	assert.NoError(t, err)
	assert.True(t, sess.IsNew)
	w := httptest.NewRecorder()
	assert.NoError(t, sess.Save(r, w))

	r2 := httptest.NewRequest("GET", "/", nil)
	r2.Header.Add("Cookie", w.Header().Get("Set-Cookie"))

	sess2, err := store.New(r2, "test")
	assert.NoError(t, err)
	assert.False(t, sess2.IsNew)
}

func TestSessionExpires(t *testing.T) {
	tmpdir, err := ioutil.TempDir("", "store-test")
	require.NoError(t, err)
	path := filepath.Join(tmpdir, "test.db")
	db, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	defer os.RemoveAll(tmpdir)

	store, err := NewStore(db, securecookie.GenerateRandomKey(32))
	require.NoError(t, err)
	store.Options = &sessions.Options{
		MaxAge: 1,
	}

	r := httptest.NewRequest("GET", "/", nil)
	sess, err := store.New(r, "test")
	assert.NoError(t, err)
	assert.True(t, sess.IsNew)
	w := httptest.NewRecorder()
	assert.NoError(t, sess.Save(r, w))

	time.Sleep(2 * time.Second)

	r2 := httptest.NewRequest("GET", "/", nil)
	r2.Header.Add("Cookie", w.Header().Get("Set-Cookie"))

	// cookie should expire, session should be new
	sess2, err := store.New(r2, "test")
	assert.NoError(t, err)
	assert.True(t, sess2.IsNew)
}

func TestSessionDelete(t *testing.T) {
	tmpdir, err := ioutil.TempDir("", "store-test")
	require.NoError(t, err)
	path := filepath.Join(tmpdir, "test.db")
	db, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	defer os.RemoveAll(tmpdir)

	store, err := NewStore(db, securecookie.GenerateRandomKey(32))
	require.NoError(t, err)

	r := httptest.NewRequest("GET", "/", nil)
	sess, err := store.New(r, "test")
	assert.NoError(t, err)
	assert.True(t, sess.IsNew)

	sess.Options = &sessions.Options{MaxAge: -1}
	w := httptest.NewRecorder()
	assert.NoError(t, sess.Save(r, w))

	r2 := httptest.NewRequest("GET", "/", nil)
	r2.Header.Add("Cookie", w.Header().Get("Set-Cookie"))

	// session should immediately expire
	sess2, err := store.New(r2, "test")
	assert.NoError(t, err)
	assert.True(t, sess2.IsNew)
}
