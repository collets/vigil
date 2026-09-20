package storage

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// Executable architecture checks only: Version/the CLI do not install this schema.
func draftDB(t *testing.T, name string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	b, err := os.ReadFile(filepath.Join("../../docs/spec", name+".sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(b)); err != nil {
		t.Fatal(err)
	}
	var enabled int
	if err = db.QueryRow("PRAGMA foreign_keys").Scan(&enabled); err != nil || enabled != 1 {
		t.Fatal("foreign keys not enforced")
	}
	return db
}
func sqlOK(t *testing.T, db *sql.DB, q string) {
	t.Helper()
	if _, err := db.Exec(q); err != nil {
		t.Fatal(err)
	}
}
func sqlReject(t *testing.T, db *sql.DB, q string) {
	t.Helper()
	if _, err := db.Exec(q); err == nil {
		t.Fatalf("invalid state accepted: %s", q)
	}
}

func TestProjectDraftInvariants(t *testing.T) {
	db := draftDB(t, "project")
	sqlOK(t, db, `INSERT INTO project VALUES('project',1,'/fixture','{}',1,1,'ready','local_only',1);
 INSERT INTO config_snapshots VALUES('config','digest',1,'{}','{}',1);
 INSERT INTO profiles VALUES('local',1,'config','{}');
 INSERT INTO plans VALUES('p',1,1,'active',1800000,1,NULL);
 INSERT INTO plans VALUES('p2',1,2,'queued',1800000,1,NULL);
 INSERT INTO plan_revisions VALUES('p',1,'spec','{}','human',1);
 INSERT INTO plan_revisions VALUES('p2',1,'spec2','{}','human',1);
 INSERT INTO tasks VALUES('t','p',1,'implementation','ready',1,NULL,2700000,2,1);
 INSERT INTO tasks VALUES('other','p2',1,'implementation','ready',1,NULL,2700000,2,1);
 INSERT INTO task_revisions VALUES('t',1,'{}','criteria','definition','human',NULL);
 INSERT INTO task_revisions VALUES('other',1,'{}','criteria2','definition2','human',NULL);
 INSERT INTO runs VALUES('r','p',1,'t',1,'config','local',1,'implementation','initial','active','unconfirmed',600000,7200000,1,1,NULL);`)
	sqlReject(t, db, `UPDATE plans SET state='active' WHERE id='p2'`)
	sqlReject(t, db, `INSERT INTO task_dependencies VALUES('p','t','other')`)
	sqlReject(t, db, `INSERT INTO task_dependencies VALUES('p','t','t')`)
	sqlReject(t, db, `INSERT INTO runs SELECT 'r2',plan_id,plan_revision,task_id,task_revision,config_id,profile_id,profile_revision,role,attempt_kind,state,writer_state,active_limit_ms,wall_limit_ms,created_at,started_at,ended_at FROM runs WHERE id='r'`)
	sqlReject(t, db, `UPDATE runs SET task_revision=99 WHERE id='r'`)
	sqlReject(t, db, `UPDATE config_snapshots SET resolved_json='{"changed":true}'`)
	sqlReject(t, db, `UPDATE task_revisions SET criteria_digest='changed'`)
	sqlOK(t, db, `INSERT INTO events VALUES(1,1,NULL,'r','generation',1,'started',1,'{}')`)
	sqlReject(t, db, `DELETE FROM events`)
	sqlReject(t, db, `UPDATE events SET kind='accepted'`)
	sqlReject(t, db, `INSERT INTO events VALUES(2,1,NULL,'r','generation',1,'duplicate',2,'{}')`)
	sqlOK(t, db, `INSERT INTO time_segments VALUES('seg','r','active',1,NULL,NULL,NULL)`)
	sqlReject(t, db, `INSERT INTO time_segments VALUES('seg2','r','active',2,NULL,NULL,NULL)`)
	sqlReject(t, db, `UPDATE time_segments SET ended_at=2,duration_ms=-1 WHERE id='seg'`)
	sqlOK(t, db, `INSERT INTO operations VALUES('op','commit','tree','args',1,'prepared','p','t','r','{}',1);
 INSERT INTO operations VALUES('op2','commit','tree2','args2',1,'prepared','p','t','r','{}',1);
 INSERT INTO grants VALUES('g','commit','once','p','t','repo','{}',1,'human',1,NULL,NULL,'op');`)
	sqlReject(t, db, `UPDATE grants SET reserved_operation='op2' WHERE id='g'`)
	sqlReject(t, db, `INSERT INTO manual_checks VALUES('m','t',1,'code','Verify','pass',NULL,NULL,NULL,NULL)`)
	sqlOK(t, db, `INSERT INTO repositories VALUES('repo','/fixture','git-id','main','base','{}','participating');
 INSERT INTO deliveries VALUES('d','p','repo','op','draft_request','prepared','remote','oid','main',NULL,NULL,1)`)
	sqlReject(t, db, `UPDATE deliveries SET draft=0 WHERE id='d'`)
	var integrity string
	if err := db.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("integrity: %s %v", integrity, err)
	}
	rows, err := db.Query("PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("foreign key violation")
	}
}

func TestCoordinationDraftCapacityAndQuarantine(t *testing.T) {
	db := draftDB(t, "coordination")
	sqlOK(t, db, `INSERT INTO instances VALUES('i','host','boot',1,'start','lock',1);
 INSERT INTO workspace_claims VALUES('claim','op','p','i','/fixture','device:inode',NULL,1,'quarantined',1,'writer unknown');
 INSERT INTO endpoints VALUES('local','http://127.0.0.1:8080/v1',1,1);
 INSERT INTO endpoints VALUES('other','http://127.0.0.1:8081/v1',1,1);
 INSERT INTO queue_tickets VALUES(1,'ticket-op','local','i','p','r',1,'reserved');
 INSERT INTO queue_tickets VALUES(2,'ticket-op2','local','i','p','r2',2,'reserved');
 INSERT INTO endpoint_slots VALUES('local',0,1,1,'quarantined');`)
	sqlReject(t, db, `INSERT INTO workspace_claims VALUES('other','op2','p2','i','/fixture','different',NULL,1,'active',1,NULL)`)
	sqlReject(t, db, `INSERT INTO endpoint_slots VALUES('local',1,2,1,'active')`)
	sqlReject(t, db, `INSERT INTO endpoint_slots VALUES('other',0,2,1,'active')`)
	sqlReject(t, db, `UPDATE endpoint_slots SET slot_number=10`)
	sqlReject(t, db, `INSERT INTO global_grants VALUES('g','push','{}',0,1,NULL)`)
	// Releasing a slot and cancelling its queue record can be rolled back together.
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`DELETE FROM endpoint_slots; UPDATE queue_tickets SET state='released'`); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM endpoint_slots WHERE state='quarantined'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("rollback lost quarantine")
	}
}
