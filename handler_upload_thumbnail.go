package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"mime"
	"net/http"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	const maxMemory = 10 << 20
	r.ParseMultipartForm(maxMemory)

	file, header, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}

	headerType := header.Header.Values("Content-Type")

	defer file.Close()

	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable read the image data", err)
		return
	}

	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse get video from db", err)
		return
	}

	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "Authenticated user is not the video owner", err)
		return
	}

	mediatype, _, err := mime.ParseMediaType(headerType[0])
	if mediatype != "image/jpeg" && mediatype != "image/png" {
		respondWithError(w, http.StatusBadRequest, "the thumbnail is not an image", err)
	}

	fileExtension := fileExtensionMaker(headerType[0])

	rnd := make([]byte, 32)
	filled, err := rand.Read(rnd)
	if filled != 32 || err != nil {
		respondWithError(w, http.StatusInternalServerError, "couldnt create file dst", err)
	}
	fileName := base64.RawURLEncoding.EncodeToString(rnd)

	dst, err := cfg.createFilePath(fileName, fileExtension)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "file to store thumbnail couldnt be created", err)
	}

	defer dst.Close()

	err = copyFileToDst(dst, file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error saving file", err)
		return
	}

	url := cfg.makeURL(fileName, fileExtension)
	video.ThumbnailURL = &url
	if err := cfg.db.UpdateVideo(video); err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to update video", err)
		return
	}

	respondWithJSON(w, http.StatusOK, video)
}
