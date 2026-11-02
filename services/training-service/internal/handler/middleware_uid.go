package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/quenching/go-common/auth"
)

const HeaderUserID = auth.HeaderUserID
const ContextUID = auth.ContextUID

func UserID() gin.HandlerFunc                 { return auth.UserID() }
func MustUserID(c *gin.Context) (int64, bool) { return auth.MustUserID(c) }
