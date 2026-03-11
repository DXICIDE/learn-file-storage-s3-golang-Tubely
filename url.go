package main

import (
	"context"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"
)

func generatePresignedURL(s3Client *s3.Client, bucket, key string, expireTime time.Duration) (string, error) {
	presignClient := s3.NewPresignClient(s3Client)
	var params s3.GetObjectInput
	params.Bucket = &bucket
	params.Key = &key
	opts := s3.WithPresignExpires(expireTime)

	presignedHTTP, err := presignClient.PresignGetObject(context.Background(), &params, opts)
	if err != nil {
		return "", err
	}
	return presignedHTTP.URL, nil
}

func (cfg *apiConfig) dbVideoToSignedVideo(video database.Video) (database.Video, error) {
	if video.VideoURL == nil {
		return video, nil
	}
	bucketKey := strings.Split(*video.VideoURL, ",")
	url, err := generatePresignedURL(cfg.s3Client, bucketKey[0], bucketKey[1], 5*time.Minute)
	if err != nil {
		return video, err
	}
	video.VideoURL = &url
	return video, nil
}
