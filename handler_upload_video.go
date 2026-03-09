package main

import (
	"crypto/rand"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<30)

	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	//authenticatiing user
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

	fmt.Println("uploading video", videoID, "by user", userID)

	//video metadata to verify if user is the owner

	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Unable to parse get video from db", err)
		return
	}

	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "Authenticated user is not the video owner", err)
		return
	}

	//parsing the uploaded video

	const maxMemory = 10 << 20
	err = r.ParseMultipartForm(maxMemory)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}

	file, header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}

	headerType := header.Header.Values("Content-Type")
	if len(headerType) == 0 {
		respondWithError(w, http.StatusBadRequest, "missing content type", nil)
		return
	}

	defer file.Close()

	mediatype, _, err := mime.ParseMediaType(headerType[0])
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	if mediatype != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "the media is not an mp4 video", err)
		return
	}

	fileTmp, err := os.CreateTemp("", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Failed to create tmp file", err)
		return
	}

	defer os.Remove(fileTmp.Name())
	defer fileTmp.Close()
	_, err = io.Copy(fileTmp, file)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Failed to copy the file to tmp", err)
		return
	}

	_, err = fileTmp.Seek(0, io.SeekStart)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Failed to reset the file pointer to start", err)
		return
	}
	var putObject s3.PutObjectInput
	rnd := make([]byte, 32)
	filled, err := rand.Read(rnd)
	if filled != 32 || err != nil {
		respondWithError(w, http.StatusInternalServerError, "couldnt create file dst", err)
	}
	fileName := fmt.Sprintf("%x.mp4", rnd)

	putObject.Bucket = &cfg.s3Bucket
	putObject.Key = &fileName
	putObject.Body = fileTmp
	putObject.ContentType = &mediatype

	_, err = cfg.s3Client.PutObject(r.Context(), &putObject)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Failed to put object in the bucket", err)
		return
	}
	videoUrl := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", cfg.s3Bucket, cfg.s3Region, fileName)
	video.VideoURL = &videoUrl
	cfg.db.UpdateVideo(video)

	respondWithJSON(w, http.StatusOK, video)

}
