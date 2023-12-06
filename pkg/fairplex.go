package fairplex

import (
	"crypto/sha1"

	"encoding/hex"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/didip/tollbooth"
	"github.com/didip/tollbooth/limiter"
	"github.com/didip/tollbooth_gin"
	rbtree "github.com/emirpasic/gods/trees/redblacktree"
	"github.com/gin-gonic/gin"
)

type Fairplex struct {
	// List of all server URLs.
	Servers []*url.URL;
	// Number of requests a user can make per minute
	RequestsPerMinute float64;
	// Server addresses are hashed and put in a red-black tree, with hash as the key
	// and address as the value.
	tree *rbtree.Tree;
	mu sync.Mutex;
}

func hash(s string) string {
	h := sha1.New()
    _, err := h.Write([]byte(s))
	if err != nil {
		log.Printf("failed to hash %v: %v\n", s, err)
	}
	return hex.EncodeToString(h.Sum(nil))[:40]
}

// Checks if the given address `addr` is valid by making a
// GET request to addr + "/ping". The server must respond with 
// a 200 OK status to be valid.
func (fairplex *Fairplex) isAddrValid(addr string) bool {
	c := http.Client{}
	u, err := url.Parse(addr)
	if err != nil {
		log.Printf("error parsing addr: %v\n", addr)
		return false
	}

	resp, err := c.Get(u.JoinPath("/ping").String())
	if err != nil {
		log.Printf("error pinging addr %v: %v\n", u.String(), err)
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// This is the main function that handles all request methods.
func (fairplex *Fairplex) balanceRequest(c *gin.Context) {
	path := c.Params.ByName("path")
	path_hash := hash(c.Request.RemoteAddr + path)

	log.Printf("client %v requesting %v\n%v", c.Request.RemoteAddr, c.Request.URL.Path, path)
	log.Printf("%v\n", path_hash)

	iter := fairplex.tree.Iterator()
	iter.Begin()
	if iter.First() {
		v := iter.Key().(string)
		selected_server := iter.Value().(*url.URL)
		if v > path_hash {
			log.Printf("selected server %v for %v\n",selected_server.String(), path)
			c.Redirect(http.StatusTemporaryRedirect, selected_server.JoinPath(path).String())
			return
		} else {
			for iter.Next() {
				v = iter.Key().(string)
				selected_server = iter.Value().(*url.URL)
				if v > path_hash {
					log.Printf("selected server %v for %v\n",selected_server.String(), path)
					c.Redirect(http.StatusTemporaryRedirect, selected_server.JoinPath(path).String())
					return
				}
			}
			log.Printf("fell-through to server %v for %v\n",selected_server.String(), path)
			c.Redirect(http.StatusTemporaryRedirect, selected_server.JoinPath(path).String())
			return
		}
	} else {
		log.Println("no servers in tree")
	}
}

// SetupRouter creates the gin.Engine object, attaching method handlers.
func (fairplex *Fairplex) SetupRouter() *gin.Engine {
	r := gin.Default()
	r.SetTrustedProxies(nil) //https://github.com/gin-gonic/gin/issues/2809

	limiter := tollbooth.NewLimiter(fairplex.RequestsPerMinute, &limiter.ExpirableOptions{DefaultExpirationTTL: time.Minute})
	limiter.SetMethods([]string{"POST"})
	limiter.SetMessage(`{"error": "too many requests"}`)
	limiter.SetMessageContentType("application/json; charset=utf-8")

	r.GET("/ping", tollbooth_gin.LimitHandler(limiter),  func(c *gin.Context) {
		c.String(http.StatusOK, "pong")
	})

	r.GET("/servers", tollbooth_gin.LimitHandler(limiter), func(c *gin.Context) {
		c.JSON(http.StatusOK, fairplex.Servers)
	})

	r.POST("/servers", tollbooth_gin.LimitHandler(limiter), func(c *gin.Context) {
		addr := c.Request.FormValue("addr")
		if !fairplex.isAddrValid(addr) {
			c.JSON(http.StatusNotAcceptable, gin.H{"status": "error", "reason": "invalid address"})
			return
		}
		
		u, err := url.Parse(addr)
		if err != nil {
			log.Printf("error parsing received server URL %v: %v", addr, err)
		}

		fairplex.mu.Lock()
		fairplex.Servers = append(fairplex.Servers, u)
		if fairplex.tree == nil {
			fairplex.tree = rbtree.NewWithStringComparator()
		}
		fairplex.tree.Put(hash(u.String() + "0"), u)
		fairplex.tree.Put(hash(u.String() + "1"), u)
		fairplex.tree.Put(hash(u.String() + "2"), u)
		fairplex.tree.Put(hash(u.String() + "3"), u)
		fairplex.mu.Unlock()

		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	r.GET("/:path", fairplex.balanceRequest)
	r.POST("/:path", fairplex.balanceRequest)
	r.PUT("/:path", fairplex.balanceRequest)
	r.OPTIONS("/:path", fairplex.balanceRequest)
	r.PATCH("/:path", fairplex.balanceRequest)
	r.DELETE("/:path", fairplex.balanceRequest)
	r.HEAD("/:path", fairplex.balanceRequest)
	
	return r
}
