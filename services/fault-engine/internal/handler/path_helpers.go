package handler

import (
	"github.com/gin-gonic/gin"
	"strconv"
)

func parsePathID(c *gin.Context, name string) (int64, error) {
	return strconv.ParseInt(c.Param(name), 10, 64)
}
