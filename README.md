# MySQL Binlog Replication with Go

## Deskripsi

Program ini merupakan implementasi replikasi binlog MySQL menggunakan Go. Program ini membaca perubahan data dari database utama (source) dan mereplikasinya ke database tujuan (destination) menggunakan binlog MySQL.

## Fitur

- Mendukung operasi **INSERT**, **UPDATE**, dan **DELETE**
- Menyimpan posisi terakhir dari binlog agar dapat melanjutkan replikasi saat program dijalankan kembali
- Mendukung shutdown yang aman dengan menangani sinyal SIGINT dan SIGTERM

## Prasyarat

- Go 1.18 atau lebih baru
- MySQL server dengan binary logging diaktifkan
- Database dengan user yang memiliki akses ke binlog

## Mengaktifkan Binlog di MySQL
Untuk mengaktifkan replikasi binlog, ubah file konfigurasi MySQL (`my.cnf` atau `my.ini`) pada database sumber:

```ini
[mysqld]
server-id=1
log-bin=mysql-bin
binlog-format=ROW
expire-logs-days=10
```

Setelah mengubah konfigurasi, restart MySQL:

```sh
systemctl restart mysql
```

### Memberikan Hak Akses Replikasi
Jalankan perintah SQL berikut pada database sumber untuk memberikan akses replikasi:

```sql
CREATE USER 'replica'@'%' IDENTIFIED BY 'Replica2025!';
GRANT REPLICATION SLAVE, REPLICATION CLIENT ON *.* TO 'replica'@'%';
FLUSH PRIVILEGES;
```

## Instalasi dan Konfigurasi

1. **Clone repository**
   ```sh
   git clone <repository-url>
   cd <repository-folder>
   ```

2. **Install dependensi**
   ```sh
   go mod tidy
   ```

3. **Konfigurasi koneksi database**
   Sesuaikan variabel berikut dalam kode **main.go**:
   ```go
   var (
       prodHost = "<hostproduction>"   // IP Database Sumber
       prodPort = <portproductiion>            // Port Database Sumber
       prodUser = "<userproduction>"      // User Database Sumber
       prodPass = "<passproduction>" // Password Database Sumber
       prodDB   = "<databasenameproduction>"    // Nama Database Sumber
   
       destHost = "<hostdsetination>"  // IP Database Tujuan
       destPort = <portdestination>          // Port Database Tujuan
       destUser = "<userdestination>"      // User Database Tujuan
       destPass = "<passdestination>" // Password Database Tujuan
       destDB   = "<databasenamedestination>" // Nama Database Tujuan
   )
   ```

4. **Jalankan program**
   ```sh
   go run main.go
   ```

## Cara Kerja

1. Program mencoba membuka koneksi ke database tujuan.
2. Program memeriksa posisi terakhir binlog yang telah diproses dari file `binlog.pos`.
3. Jika tidak ada posisi tersimpan, program mendapatkan posisi terbaru dari binlog.
4. Program memulai sinkronisasi binlog dan menangani event berikut:
   - **INSERT**: Menyisipkan data baru ke database tujuan.
   - **UPDATE**: Memperbarui data berdasarkan primary key.
   - **DELETE**: Menghapus data berdasarkan primary key.
5. Posisi binlog diperbarui setiap kali event diproses untuk menjaga konsistensi.
6. Program dapat dihentikan dengan aman menggunakan Ctrl+C atau SIGTERM.

## Struktur Kode

- **main()**: Fungsi utama yang mengatur koneksi database dan memulai sinkronisasi binlog.
- **processEvent()**: Menangani event dari binlog.
- **getPrimaryKeyColumn()**: Mendapatkan primary key dari tabel.
- **processUpdateEvent()**: Menangani update dengan mempertahankan primary key.
- **generateInsertQuery()**: Membuat query untuk operasi INSERT dengan duplicate key handling.
- **getCurrentBinlogPosition()**: Mengambil posisi binlog terbaru dari database sumber.
- **saveBinlogPosition()**: Menyimpan posisi binlog ke file.
- **loadBinlogPosition()**: Memuat posisi binlog dari file.

## Penanganan Error

- Jika terjadi error saat memproses event binlog, program akan mencatat log dan mencoba melanjutkan proses.
- Jika gagal terhubung ke database tujuan, program akan keluar dengan pesan error.
- Jika terjadi error dalam membaca posisi binlog, program akan mendapatkan posisi terbaru dari database sumber.

## Catatan

- Hanya data yang ditambahkan setelah layanan ini berjalan yang akan direplikasi.
- Data yang sudah ada sebelum layanan ini berjalan tidak akan tertangkap dalam binlog dan tidak akan direplikasi.




