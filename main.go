package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-mysql-org/go-mysql/mysql"
	"github.com/go-mysql-org/go-mysql/replication"
	_ "github.com/go-sql-driver/mysql"
)

var (
	prodHost = "10.2.7.108"
	prodPort = 3306
	prodUser = "replica"
	prodPass = "Replica2025!"
	prodDB   = "kikomunal"

	destHost = "10.1.35.243"
	destPort = 3306
	destUser = "replica"
	destPass = "Replica2025!"
	destDB   = "kikomunaldest"

	db2 *sql.DB
)

const posFile = "binlog.pos"

func main() {
	var err error
	db2, err = sql.Open("mysql", fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true", destUser, destPass, destHost, destPort, destDB))
	if err != nil {
		log.Fatal("Gagal koneksi ke db_2:", err)
	}
	defer db2.Close()

	cfg := replication.BinlogSyncerConfig{
		ServerID:  100,
		Flavor:    "mysql",
		Host:      prodHost,
		Port:      uint16(prodPort),
		User:      prodUser,
		Password:  prodPass,
		ParseTime: true,
	}

	syncer := replication.NewBinlogSyncer(cfg)
	defer syncer.Close()

	pos, err := loadBinlogPosition()
	if err != nil {
		pos, err = getCurrentBinlogPosition()
		if err != nil {
			log.Fatal("Error mendapatkan posisi binlog:", err)
		}
	}

	streamer, err := syncer.StartSync(pos)
	if err != nil {
		log.Fatal("Error memulai sinkronisasi binlog:", err)
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		log.Println("Shutdown signal diterima, menutup syncer...")
		syncer.Close()
		os.Exit(0)
	}()

	log.Println("Memulai replikasi...")

	for {
		ev, err := streamer.GetEvent(context.Background())
		if err != nil {
			log.Printf("Error mendapatkan event: %v", err)
			time.Sleep(time.Second)
			continue
		}

		currentPos := mysql.Position{
			Name: pos.Name,
			Pos:  ev.Header.LogPos,
		}
		if err := saveBinlogPosition(currentPos); err != nil {
			log.Printf("Gagal menyimpan posisi binlog: %v", err)
		}
		pos = currentPos

		switch e := ev.Event.(type) {
		case *replication.RowsEvent:
			processEvent(string(e.Table.Schema), string(e.Table.Table), ev.Header.EventType, e.Rows)
		}
	}
}

func processEvent(dbName, tableName string, eventType replication.EventType, rows [][]interface{}) {
	if dbName != prodDB {
		return
	}

	switch eventType {
	case replication.WRITE_ROWS_EVENTv1, replication.WRITE_ROWS_EVENTv2:
		for _, row := range rows {
			query, args := generateInsertQuery(tableName, row)
			_, err := db2.Exec(query, args...)
			if err != nil {
				log.Printf("Gagal INSERT %s: %v", tableName, err)
			}
		}

	case replication.UPDATE_ROWS_EVENTv1, replication.UPDATE_ROWS_EVENTv2:
		processUpdateEvent(tableName, rows)

	case replication.DELETE_ROWS_EVENTv1, replication.DELETE_ROWS_EVENTv2:
		pk, err := getPrimaryKeyColumn(tableName)
		if err != nil {
			log.Printf("Gagal mendapatkan primary key untuk %s: %v", tableName, err)
			return
		}
		for _, row := range rows {
			query := fmt.Sprintf("DELETE FROM `%s` WHERE `%s`=?", tableName, pk)
			_, err := db2.Exec(query, row[0])
			if err != nil {
				log.Printf("Gagal DELETE %s: %v", tableName, err)
			}
		}
	}
}

func getColumnNames(tableName string) ([]string, error) {
	query := fmt.Sprintf("SHOW COLUMNS FROM `%s`", tableName)
	rows, err := db2.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var field, typ, null, key, extra string
		var def sql.NullString
		if err := rows.Scan(&field, &typ, &null, &key, &def, &extra); err != nil {
			return nil, err
		}
		columns = append(columns, field)
	}
	return columns, nil
}

func getPrimaryKeyColumn(tableName string) (string, error) {
	query := `
		SELECT COLUMN_NAME 
		FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE 
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND CONSTRAINT_NAME = 'PRIMARY' 
		LIMIT 1`
	var col string
	err := db2.QueryRow(query, destDB, tableName).Scan(&col)
	if err != nil {
		return "", fmt.Errorf("error mendapatkan primary key: %v", err)
	}
	return col, nil
}

func processUpdateEvent(tableName string, rows [][]interface{}) {
	columns, err := getColumnNames(tableName)
	if err != nil {
		log.Printf("Gagal mendapatkan kolom untuk %s: %v", tableName, err)
		return
	}

	pk, err := getPrimaryKeyColumn(tableName)
	if err != nil {
		log.Printf("Gagal mendapatkan primary key untuk %s: %v", tableName, err)
		return
	}

	// Cari index primary key
	pkIndex := -1
	for i, col := range columns {
		if col == pk {
			pkIndex = i
			break
		}
	}
	if pkIndex == -1 {
		log.Printf("Primary key %s tidak ditemukan di tabel %s", pk, tableName)
		return
	}

	// Generate SET clause tanpa primary key
	var setColumns []string
	var valueIndexes []int
	for i, col := range columns {
		if col != pk {
			setColumns = append(setColumns, fmt.Sprintf("`%s`=?", col))
			valueIndexes = append(valueIndexes, i)
		}
	}

	query := fmt.Sprintf("UPDATE `%s` SET %s WHERE `%s`=?",
		tableName, strings.Join(setColumns, ","), pk)

	for i := 0; i < len(rows); i += 2 {
		newRow := rows[i+1]
		var args []interface{}
		for _, idx := range valueIndexes {
			args = append(args, newRow[idx])
		}
		args = append(args, newRow[pkIndex])

		_, err := db2.Exec(query, args...)
		if err != nil {
			log.Printf("Gagal UPDATE %s: %v", tableName, err)
		}
	}
}

func generateInsertQuery(table string, row []interface{}) (string, []interface{}) {
	pk, err := getPrimaryKeyColumn(table)
	if err != nil {
		log.Printf("Gagal mendapatkan primary key untuk %s: %v", table, err)
		return "", nil
	}

	placeholders := strings.Repeat("?,", len(row)-1) + "?"
	query := fmt.Sprintf("INSERT INTO `%s` VALUES (%s) ON DUPLICATE KEY UPDATE `%s`=VALUES(`%s`)",
		table, placeholders, pk, pk)
	return query, row
}

func getCurrentBinlogPosition() (mysql.Position, error) {
	conn, err := sql.Open("mysql", fmt.Sprintf("%s:%s@tcp(%s:%d)/", prodUser, prodPass, prodHost, prodPort))
	if err != nil {
		return mysql.Position{}, err
	}
	defer conn.Close()

	var file string
	var pos uint32
	err = conn.QueryRow("SHOW MASTER STATUS").Scan(&file, &pos, new(interface{}), new(interface{}), new(interface{}))
	if err != nil {
		return mysql.Position{}, err
	}
	return mysql.Position{Name: file, Pos: pos}, nil
}

func saveBinlogPosition(pos mysql.Position) error {
	return os.WriteFile(posFile, []byte(fmt.Sprintf("%s;%d", pos.Name, pos.Pos)), 0644)
}

func loadBinlogPosition() (mysql.Position, error) {
	data, err := os.ReadFile(posFile)
	if err != nil {
		return mysql.Position{}, err
	}
	parts := strings.Split(string(data), ";")
	if len(parts) < 2 {
		return mysql.Position{}, fmt.Errorf("format file posisi tidak valid")
	}
	posInt, err := strconv.Atoi(parts[1])
	if err != nil {
		return mysql.Position{}, err
	}
	return mysql.Position{Name: parts[0], Pos: uint32(posInt)}, nil
}
