package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"

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

	//parses the media type
	mediatype, _, err := mime.ParseMediaType(headerType[0])
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	if mediatype != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "the media is not an mp4 video", err)
		return
	}

	//creates temporary file for thatll substitute the original file
	fileTmp, err := os.CreateTemp("", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Failed to create tmp file", err)
		return
	}

	//removing the file at the end
	defer os.Remove(fileTmp.Name())
	defer fileTmp.Close()

	//copying file
	_, err = io.Copy(fileTmp, file)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Failed to copy the file to tmp", err)
		return
	}

	//resetting pointer
	_, err = fileTmp.Seek(0, io.SeekStart)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Failed to reset the file pointer to start", err)
		return
	}

	//making Input for s3 and randomizing file name for s3
	var putObject s3.PutObjectInput
	rnd := make([]byte, 32)
	filled, err := rand.Read(rnd)
	if filled != 32 || err != nil {
		respondWithError(w, http.StatusInternalServerError, "couldnt create file dst", err)
		return
	}

	//moving the moov atom at the start
	fileName, err := processVideoForFastStart(fileTmp.Name())
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Failed to put moov Atom at the start of file", err)
		return
	}
	processedFile, err := os.Open(fileName)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Failed to open the new file", err)
		return
	}
	defer os.Remove(processedFile.Name())
	defer processedFile.Close()

	//putting prefix infront of the file name based on aspect ratio for better clarity
	aspectRatio, err := getVideoAspectRatio(processedFile.Name())
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Failed to reset the file pointer to start", err)
		return
	}
	fileName = fmt.Sprintf("%x.mp4", rnd)
	if aspectRatio == "16:9" {
		fileName = fmt.Sprintf("landscape/%s", fileName)
	} else if aspectRatio == "9:16" {
		fileName = fmt.Sprintf("portrait/%s", fileName)
	} else if aspectRatio == "other" {
		fileName = fmt.Sprintf("other/%s", fileName)
	}

	//setting the s3 bucket input
	putObject.Bucket = &cfg.s3Bucket
	putObject.Key = &fileName
	putObject.Body = processedFile
	putObject.ContentType = &mediatype

	//putting the object in the bucket
	_, err = cfg.s3Client.PutObject(r.Context(), &putObject)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Failed to put object in the bucket", err)
		return
	}
	videoUrl := fmt.Sprintf("%s,%s", cfg.s3Bucket, fileName)
	video.VideoURL = &videoUrl
	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Failed to update db", err)
		return
	}

	video, err = cfg.dbVideoToSignedVideo(video)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Failed to put create signed url", err)
		return
	}

	respondWithJSON(w, http.StatusOK, video)

}

func getVideoAspectRatio(filePath string) (string, error) {
	type ffprobeOutput struct {
		Streams []struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"streams"`
	}

	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	var buffer bytes.Buffer
	cmd.Stdout = &buffer
	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("ffprobe error: %v", err)
	}

	var jsonStruct ffprobeOutput
	err = json.Unmarshal(buffer.Bytes(), &jsonStruct)
	if err != nil {
		return "", err
	}
	var ratio float32
	ratio = float32(jsonStruct.Streams[0].Width) / float32(jsonStruct.Streams[0].Height)
	if len(jsonStruct.Streams) > 0 && 1.75 < ratio && ratio < 1.8 {
		return "16:9", nil
	}
	if len(jsonStruct.Streams) > 0 && 0.54 < ratio && ratio < 0.58 {
		return "9:16", nil
	}
	if len(jsonStruct.Streams) < 1 {
		return "", errors.New("no streams found in video")
	}
	return "other", nil
}

func processVideoForFastStart(filePath string) (string, error) {
	newOutput := filePath + ".processing"
	cmd := exec.Command("ffmpeg", "-i", filePath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", newOutput)

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("ffmpegz error: %v", err)
	}
	return newOutput, nil
}
