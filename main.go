package main

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/go-chi/jwtauth/v5"
	_ "github.com/lib/pq"
)

var (
	dbProtocol = "postgresql"
	dbHost     = "localhost"
	dbPort     = "5432"
	dbUser     = "fmngr"
	dbPassword = "fmngr_password"
	dbName     = "fmngr"
)

var db *sql.DB
var accessToken *jwtauth.JWTAuth

type storage struct {
	Id        int    `json:"id"`
	Path      string `json:"path"`
	IsDefault bool   `json:"is_default"`
}

type file struct {
	Id        int    `json:"id"`
	Title     string `json:"title"`
	Size      int64  `json:"size"`
	Ext       string `json:"ext"`
	StorageId int    `json:"storage_id"`
}

func init() {
	accessToken = jwtauth.New("HS256", []byte("secret"), nil)
	_, tokenString, _ := accessToken.Encode(map[string]interface{}{"user_id": 1})
	fmt.Printf("DEBUG: a sample jwt is %s\n\n", tokenString)
}

func main() {
	connStr := fmt.Sprintf("%s://%s:%s@%s:%s/%s", dbProtocol, dbUser, dbPassword, dbHost, dbPort, dbName)

	var err error
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// ping the db
	err = db.Ping()
	if err != nil {
		log.Fatal(err)
	}
	log.Print("connected to the database")

	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.CleanPath)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{"https://*", "http://*"},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Accept", "Authorization", "Content-Type"},
		ExposedHeaders: []string{"Link"},
	}))
	r.Use(middleware.Recoverer)

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello"))
	})

	r.Route("/auth", func(r chi.Router) {
		r.Post("/register", register)
		r.Post("/tokens/access", authToken)
	})

	r.Route("/storage", func(r chi.Router) {
		r.Post("/", storageCreate)
		r.Get("/", storageList)
		r.Get("/{storage_id}", storageSingle)
		r.Put("/{storage_id}", storageModify)
		r.Delete("/{storage_id}", storageDelete)
	})

	r.Route("/files", func(r chi.Router) {
		r.Post("/", fileCreate)
		r.Get("/", fileList)
		r.Get("/{file_id}", fileSingle)
		r.Delete("/{file_id}", fileDelete)
	})

	log.Print("http server running on port 5000")
	http.ListenAndServe(":5000", r)
}

// auth funcs

func register(w http.ResponseWriter, r *http.Request) {
	type request struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	w.Write([]byte("register"))
}

func authToken(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("token"))
}

// storage crud funcs

func storageCreate(w http.ResponseWriter, r *http.Request) {
	type request struct {
		Path      string `json:"path"`
		IsDefault bool   `json:"is_default"`
	}
	var rBody request
	err := json.NewDecoder(r.Body).Decode(&rBody)
	if err != nil {
		log.Print(err)
	}

	var s storage

	err = db.QueryRow("INSERT INTO storage (path, is_default) VALUES ($1, $2) RETURNING id, path, is_default", rBody.Path, rBody.IsDefault).Scan(&s.Id, &s.Path, &s.IsDefault)
	if err != nil {
		log.Print(err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s)
}

func storageList(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query("SELECT id, path, is_default FROM storage")
	if err != nil {
		log.Print(err)
	}
	defer rows.Close()

	storageList := []storage{}
	for rows.Next() {
		var s storage
		err := rows.Scan(&s.Id, &s.Path, &s.IsDefault)
		if err != nil {
			log.Println(err)
		}
		storageList = append(storageList, s)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(storageList)
}

func storageSingle(w http.ResponseWriter, r *http.Request) {
	storageIdStr := chi.URLParam(r, "storage_id")
	storageId, err := strconv.Atoi(storageIdStr)
	if err != nil {
		log.Print(err)
	}
	fmt.Println(storageId)

	w.Write([]byte("single storage"))
}

func storageModify(w http.ResponseWriter, r *http.Request) {
	// todo
}

func storageDelete(w http.ResponseWriter, r *http.Request) {
	// todo
}

// files crud funcs

func fileCreate(w http.ResponseWriter, r *http.Request) {
	// get request file
	formFile, handler, err := r.FormFile("file")
	if err != nil {
		fmt.Println(err)
	}
	defer formFile.Close()

	// get default storage info
	var s storage
	err = db.QueryRow("SELECT id, path, is_default FROM storage WHERE is_default = true").Scan(&s.Id, &s.Path, &s.IsDefault)
	if err != nil {
		log.Print(err)
	}

	// define file system path and create the new file
	p := filepath.Join(s.Path, handler.Filename)
	newFile, err := os.Create(p)
	if err != nil {
		fmt.Println(err)
	}
	defer newFile.Close()

	// copy file content
	_, err = io.Copy(newFile, formFile)
	if err != nil {
		fmt.Println(err)
	}

	// get new file info to store it in the db
	fInfo, err := newFile.Stat()
	if err != nil {
		log.Print(err)
	}
	fName := fInfo.Name()
	fSize := fInfo.Size()
	fExt := filepath.Ext(fName)
	fTitle := fName[:len(fName)-len(fExt)]

	var f file

	err = db.QueryRow("INSERT INTO file (title, size, ext, storage_id) VALUES ($1, $2, $3, $4) RETURNING id, title, size, ext, storage_id", fTitle, fSize, fExt, s.Id).Scan(&f.Id, &f.Title, &f.Size, &f.Ext, &f.StorageId)
	if err != nil {
		log.Print(err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(f)
}

func fileList(w http.ResponseWriter, r *http.Request) {
	// query default storage
	var s storage
	err := db.QueryRow("SELECT id, path, is_default FROM storage WHERE is_default = true").Scan(&s.Id, &s.Path, &s.IsDefault)
	if err != nil {
		log.Print(err)
	}

	// query files
	rows, err := db.Query("SELECT id, title, size, ext, storage_id FROM file WHERE storage_id = $1", s.Id)
	if err != nil {
		log.Print(err)
	}

	var files []file
	for rows.Next() {
		var file file
		err := rows.Scan(&file.Id, &file.Title, &file.Size, &file.Ext, &file.StorageId)
		if err != nil {
			log.Print(err)
		}
		files = append(files, file)
	}

	json.NewEncoder(w).Encode(files)
}

func fileSingle(w http.ResponseWriter, r *http.Request) {
	fileIdStr := chi.URLParam(r, "file_id")
	fileId, err := strconv.Atoi(fileIdStr)
	if err != nil {
		log.Print(err)
	}

	// query file
	var f file
	err = db.QueryRow("SELECT id, title, size, ext, storage_id FROM file WHERE id = $1", fileId).Scan(&f.Id, &f.Title, &f.Size, &f.Ext, &f.StorageId)
	if err != nil {
		log.Print(err)
	}

	// query file storage
	var s storage
	err = db.QueryRow("SELECT id, path, is_default FROM storage WHERE id = $1", f.StorageId).Scan(&s.Id, &s.Path, &s.IsDefault)
	if err != nil {
		log.Println(err)
	}

	type fileWithBase64 struct {
		file
		Base64Value string `json:"base64_value"`
	}

	fName := f.Title + f.Ext
	fp := filepath.Join(s.Path, fName)
	fData, err := os.ReadFile(fp)
	if err != nil {
		log.Print(err)
	}
	enc := base64.StdEncoding.EncodeToString(fData)

	fResp := fileWithBase64{
		file: file{
			Id:        f.Id,
			Title:     f.Title,
			Size:      f.Size,
			Ext:       f.Ext,
			StorageId: f.StorageId,
		},
		Base64Value: enc,
	}

	json.NewEncoder(w).Encode(fResp)
}

func fileDelete(w http.ResponseWriter, r *http.Request) {
	fileIdStr := chi.URLParam(r, "file_id")
	fileId, err := strconv.Atoi(fileIdStr)
	if err != nil {
		log.Print(err)
	}

	var f file
	err = db.QueryRow("SELECT id, title, size, ext, storage_id FROM file WHERE id = $1", fileId).Scan(&f.Id, &f.Title, &f.Size, &f.Ext, &f.StorageId)
	if err != nil {
		log.Println(err)
	}

	var s storage
	err = db.QueryRow("SELECT id, path, is_default FROM storage WHERE id = $1", f.StorageId).Scan(&s.Id, &s.Path, &s.IsDefault)
	if err != nil {
		log.Println(err)
	}

	fName := f.Title + f.Ext
	fp := filepath.Join(s.Path, fName)
	err = os.Remove(fp)
	if err != nil {
		log.Print(err)
	}

	_, err = db.Exec("DELETE FROM file WHERE id = $1", f.Id)
	if err != nil {
		log.Print(err)
	}

	w.WriteHeader(http.StatusOK)
}
