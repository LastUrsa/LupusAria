CREATE TABLE IF NOT EXISTS schema_version(version INTEGER NOT NULL);
INSERT INTO schema_version SELECT 1 WHERE NOT EXISTS(SELECT 1 FROM schema_version);
CREATE TABLE IF NOT EXISTS sessions(
 id INTEGER PRIMARY KEY, broadcaster TEXT NOT NULL, stream TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('ACTIVE','END_PENDING','RECOVERING','CLOSED')),
 generation INTEGER NOT NULL DEFAULT 1 CHECK(generation>0), next_number INTEGER NOT NULL DEFAULT 1 CHECK(next_number>0),
 created INTEGER NOT NULL, first_offline INTEGER, last_offline INTEGER, last_checked INTEGER, closed INTEGER,
 UNIQUE(broadcaster,stream));
CREATE UNIQUE INDEX IF NOT EXISTS one_open_session ON sessions(broadcaster) WHERE state!='CLOSED';
CREATE TABLE IF NOT EXISTS entries(
 session INTEGER NOT NULL REFERENCES sessions(id), generation INTEGER NOT NULL,
 user_id TEXT NOT NULL, login TEXT NOT NULL, name TEXT NOT NULL, number INTEGER NOT NULL CHECK(number>0), entered INTEGER NOT NULL,
 PRIMARY KEY(session,generation,user_id), UNIQUE(session,generation,number));
CREATE TABLE IF NOT EXISTS draws(
 id INTEGER PRIMARY KEY, session INTEGER NOT NULL REFERENCES sessions(id), generation INTEGER NOT NULL,
 message_id TEXT NOT NULL, number INTEGER NOT NULL CHECK(number>0), user_id TEXT NOT NULL, name TEXT NOT NULL,
 count INTEGER NOT NULL CHECK(count>0), drawn INTEGER NOT NULL,
 announcement TEXT NOT NULL CHECK(announcement IN ('pending','sent','failed')), UNIQUE(session,message_id));
CREATE TABLE IF NOT EXISTS commands(
 session INTEGER NOT NULL REFERENCES sessions(id), message_id TEXT NOT NULL, kind TEXT NOT NULL, processed INTEGER NOT NULL,
 PRIMARY KEY(session,message_id));
CREATE TABLE IF NOT EXISTS notifications(id TEXT PRIMARY KEY, received INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS revision(value INTEGER NOT NULL);
INSERT INTO revision SELECT 0 WHERE NOT EXISTS(SELECT 1 FROM revision);
