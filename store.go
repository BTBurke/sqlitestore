/* Gorilla Sessions backend for Sqlite3.

Copyright (c) 2013 Contributors. See the list of contributors in the CONTRIBUTORS file for details.

This software is licensed under a MIT style license available in the LICENSE file.
*/
package sqlitestore

import (
	"database/sql"
	"encoding/gob"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
	_ "github.com/mattn/go-sqlite3"
)

var SessionExpired error = errors.New("session expired")

type Store struct {
	db     DB
	create *sql.Stmt
	delete *sql.Stmt
	update *sql.Stmt
	get    *sql.Stmt
	mu     sync.RWMutex

	Codecs  []securecookie.Codec
	Options *sessions.Options
}

type sessionRow struct {
	id         int
	data       string
	createdOn  time.Time
	modifiedOn time.Time
	expiresOn  time.Time
}

type DB interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
	Prepare(query string) (*sql.Stmt, error)
	Close() error
}

func init() {
	gob.Register(time.Time{})
}

func NewStore(db DB, keyPairs ...[]byte) (*Store, error) {
	cTableQ := "CREATE TABLE IF NOT EXISTS sessions " +
		"(id INTEGER PRIMARY KEY, " +
		"session_data LONGBLOB, " +
		"created_on TIMESTAMP DEFAULT 0, " +
		"modified_on TIMESTAMP DEFAULT CURRENT_TIMESTAMP, " +
		"expires_on TIMESTAMP DEFAULT 0);"
	if _, err := db.Exec(cTableQ); err != nil {
		return nil, err
	}

	insQ := "INSERT INTO sessions (id, session_data, created_on, modified_on, expires_on) VALUES (NULL, ?, ?, ?, ?)"
	create, err := db.Prepare(insQ)
	if err != nil {
		return nil, err
	}

	delQ := "DELETE FROM sessions WHERE id = ?"
	del, err := db.Prepare(delQ)
	if err != nil {
		return nil, err
	}

	updQ := "UPDATE sessions SET session_data = ?, created_on = ?, expires_on = ? " +
		"WHERE id = ?"
	update, err := db.Prepare(updQ)
	if err != nil {
		return nil, err
	}

	selQ := "SELECT id, session_data, created_on, modified_on, expires_on from sessions WHERE id = ?"
	get, stmtErr := db.Prepare(selQ)
	if stmtErr != nil {
		return nil, stmtErr
	}

	return &Store{
		db:     db,
		create: create,
		delete: del,
		update: update,
		get:    get,
		Codecs: securecookie.CodecsFromPairs(keyPairs...),
		Options: &sessions.Options{
			Path:   "/",
			MaxAge: 60 * 60 * 24 * 14,
		},
	}, nil
}

func (m *Store) Close() {
	m.get.Close()
	m.update.Close()
	m.delete.Close()
	m.create.Close()
	m.db.Close()
}

func (m *Store) Get(r *http.Request, name string) (*sessions.Session, error) {
	return sessions.GetRegistry(r).Get(m, name)
}

func (m *Store) New(r *http.Request, name string) (*sessions.Session, error) {
	session := sessions.NewSession(m, name)
	session.Options = &sessions.Options{
		Path:   m.Options.Path,
		MaxAge: m.Options.MaxAge,
	}
	session.IsNew = true
	var err error
	if cook, errCookie := r.Cookie(name); errCookie == nil {
		err = securecookie.DecodeMulti(name, cook.Value, &session.ID, m.Codecs...)
		if err == nil {
			err = m.load(session)
			if err == nil {
				session.IsNew = false
			} else {
				err = nil
			}
		}
	}
	return session, err
}

func (m *Store) Save(r *http.Request, w http.ResponseWriter, session *sessions.Session) error {
	// in accordance with the sessions spec, a MaxAge <=0 triggers deleting the cookie from storage
	// and should also cause the browser to delete the cookie
	if session.Options.MaxAge <= 0 {
		return m.Delete(r, w, session)
	}

	var err error
	if session.ID == "" {
		if err = m.insert(session); err != nil {
			return err
		}
	} else if err = m.save(session); err != nil {
		return err
	}
	encoded, err := securecookie.EncodeMulti(session.Name(), session.ID, m.Codecs...)
	if err != nil {
		return err
	}
	http.SetCookie(w, sessions.NewCookie(session.Name(), encoded, session.Options))
	return nil
}

func (m *Store) insert(session *sessions.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var createdOn time.Time
	var modifiedOn time.Time
	var expiresOn time.Time
	crOn := session.Values["created_on"]
	if crOn == nil {
		createdOn = time.Now()
	} else {
		createdOn = crOn.(time.Time)
	}
	modifiedOn = createdOn
	exOn := session.Values["expires_on"]
	if exOn == nil {
		expiresOn = time.Now().Add(time.Second * time.Duration(session.Options.MaxAge))
	} else {
		expiresOn = exOn.(time.Time)
	}
	delete(session.Values, "created_on")
	delete(session.Values, "expires_on")
	delete(session.Values, "modified_on")

	encoded, encErr := securecookie.EncodeMulti(session.Name(), session.Values, m.Codecs...)
	if encErr != nil {
		return encErr
	}
	res, insErr := m.create.Exec(encoded, createdOn, modifiedOn, expiresOn)
	if insErr != nil {
		return insErr
	}
	lastInserted, lInsErr := res.LastInsertId()
	if lInsErr != nil {
		return lInsErr
	}
	session.ID = fmt.Sprintf("%d", lastInserted)
	return nil
}

func (m *Store) Delete(r *http.Request, w http.ResponseWriter, session *sessions.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Set cookie to expire.
	options := *session.Options
	options.MaxAge = -1
	http.SetCookie(w, sessions.NewCookie(session.Name(), "", &options))
	// Clear session values.
	for k := range session.Values {
		delete(session.Values, k)
	}

	_, delErr := m.delete.Exec(session.ID)
	if delErr != nil {
		return delErr
	}
	return nil
}

func (m *Store) save(session *sessions.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if session.IsNew {
		return m.insert(session)
	}
	var createdOn time.Time
	var expiresOn time.Time
	crOn := session.Values["created_on"]
	if crOn == nil {
		createdOn = time.Now()
	} else {
		createdOn = crOn.(time.Time)
	}

	exOn := session.Values["expires_on"]
	if exOn == nil {
		expiresOn = time.Now().Add(time.Second * time.Duration(session.Options.MaxAge))
	} else {
		expiresOn = exOn.(time.Time)
		if expiresOn.Sub(time.Now().Add(time.Second*time.Duration(session.Options.MaxAge))) < 0 {
			expiresOn = time.Now().Add(time.Second * time.Duration(session.Options.MaxAge))
		}
	}

	delete(session.Values, "created_on")
	delete(session.Values, "expires_on")
	delete(session.Values, "modified_on")
	encoded, encErr := securecookie.EncodeMulti(session.Name(), session.Values, m.Codecs...)
	if encErr != nil {
		return encErr
	}
	_, updErr := m.update.Exec(encoded, createdOn, expiresOn, session.ID)
	if updErr != nil {
		return updErr
	}
	return nil
}

func (m *Store) load(session *sessions.Session) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	row := m.get.QueryRow(session.ID)
	sess := sessionRow{}
	scanErr := row.Scan(&sess.id, &sess.data, &sess.createdOn, &sess.modifiedOn, &sess.expiresOn)
	if scanErr != nil {
		return scanErr
	}
	if time.Until(sess.expiresOn) < 0 {
		return SessionExpired
	}
	err := securecookie.DecodeMulti(session.Name(), sess.data, &session.Values, m.Codecs...)
	if err != nil {
		return err
	}
	session.Values["created_on"] = sess.createdOn
	session.Values["modified_on"] = sess.modifiedOn
	session.Values["expires_on"] = sess.expiresOn
	return nil

}
