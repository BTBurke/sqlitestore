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

	w := httptest.NewRecorder()
	assert.NoError(t, sess.Save(r, w))

	r2 := httptest.NewRequest("GET", "/", nil)
	goodCookie := w.Header().Get("Set-Cookie")
	r2.Header.Add("Cookie", goodCookie)

	// session should be ok on second request, then set to expire
	sess2, err := store.New(r2, "test")
	assert.NoError(t, err)
	assert.False(t, sess2.IsNew)
	sess2.Options = &sessions.Options{MaxAge: -1}
	assert.NoError(t, sess2.Save(r2, w))

	// on third request, should be deleted and be a new session
	r3 := httptest.NewRequest("GET", "/", nil)
	r3.Header.Add("Cookie", goodCookie)
	sess3, err := store.New(r3, "test")
	assert.NoError(t, err)
	assert.True(t, sess3.IsNew)
}
