package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net/mail"
	"os"
	"path/filepath"
	"strings"

	kval "github.com/kval-access-language/kval-boltdb"
	"github.com/urfave/cli/v3"
)

// A built-in way to change the symbol denoting a sub-bucket.
// Using a folder emoji instead, for readability purposes.
// var bucketSymbol = "*" // old way of displaying sub-buckets.
var bucketSymbol = "📁" // using an emoji to stand out.

// Terminal lines...
const instructionLine = "> Enter bucket to explore (CTRL-X to quit, CTRL-B to go back, ENTER to go back to ROOT Bucket):"
const goingBack = "> Going back..."

func main() {
	var file string
	var noValues bool
	var useMore bool

	cli.CommandHelpTemplate = `NAME:
  {{.Name}} - {{.Usage}}

VERSION:
  {{.Version}}

USAGE:
  {{.HelpName}} {{if .VisibleFlags}}[global options]{{end}} [FILE]

GLOBAL OPTIONS:
  {{range .VisibleFlags}}{{.}}
  {{end}}
AUTHOR:
  {{range .Authors}}{{ . }}{{end}}
COPYRIGHT:
  {{.Copyright}}
`
	cmd := &cli.Command{
		Name:    filepath.Base(os.Args[0]),
		Usage:   "view boltdb file interactively in your terminal",
		Version: "2.0.2",
		Authors: []any{
			&mail.Address{Name: "Hasit Mistry", Address: "hasitnm@gmail.com"},
		},
		Copyright:              "(c) 2016–2025 Hasit Mistry",
		UseShortOptionHandling: true,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "file",
				Aliases:     []string{"f"},
				Usage:       "boltdb `FILE` to view (flag may be omitted)",
				Destination: &file,
			},
			&cli.BoolFlag{
				Name:        "no-values",
				Usage:       "use if values are huge and/or not printable",
				Value:       false,
				Destination: &noValues,
			},
			&cli.BoolFlag{
				Name:        "more",
				Usage:       "use `more` to print all listings. Should be available in path",
				Value:       false,
				Destination: &useMore,
			},
			&cli.StringFlag{
				Name:        "separator",
				Aliases:     []string{"s", "sep"},
				Usage:       "`symbol` for marking sub-buckets",
				Value:       "📁",
				Destination: &bucketSymbol,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if file == "" {
				// try to use the first non-flag parameter as filename:
				if !cmd.Args().Present() {
					cli.ShowRootCommandHelp(cmd)
					return nil
				}
				file = cmd.Args().First()
				if file == "" {
					cli.ShowRootCommandHelp(cmd)
					return nil
				}
			}

			var formatter formatter = &tableFormatter{
				noValues: noValues,
			}
			if useMore {
				formatter = &moreWrapFormatter{
					formatter: formatter,
				}
			}

			var i impl
			i = impl{fmt: formatter}
			if _, err := os.Stat(file); os.IsNotExist(err) {
				log.Fatal(err)
				return err
			}
			i.initDB(file)
			defer kval.Disconnect(i.kb)

			i.readInput()

			return nil
		},
		CommandNotFound: func(ctx context.Context, cmd *cli.Command, command string) {
			cli.ShowRootCommandHelp(cmd)
		},
		OnUsageError: func(ctx context.Context, cmd *cli.Command, err error, isSubcommand bool) error {
			cli.ShowRootCommandHelp(cmd)
			return err
		},
	}
	if err := cmd.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}

// Interactively reads commands from os.Stdin.
// TODO(gwyneth): replace this with https://github.com/chzyer/readline
func (i *impl) readInput() {
	i.listBuckets()
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		bucket := scanner.Text()
		fmt.Fprintln(os.Stdout, "")
		switch bucket {
		case "\x18": // cancel Ctrl-X
			fmt.Fprintln(os.Stdout, "quitting...")
			return
		case "\x02": // back Ctrl-B
			if len(i.loc) == 0 || !strings.Contains(i.loc, ">>") {
				fmt.Fprintf(os.Stdout, "%s\n", goingBack)
				i.loc = ""
				i.listBuckets()
			} else {
				i.listBucketItems(bucket, true)
			}
		case "":
			i.listBuckets()
		default:
			i.listBucketItems(bucket, false)
		}
		bucket = ""
	}
}

type formatter interface {
	DumpBuckets(io.Writer, []bucket)
	DumpBucketItems(io.Writer, string, []item)
}

type impl struct {
	kb     kval.Kvalboltdb
	fmt    formatter
	bucket string
	loc    string // navigation, what is our requested location in the store?
	cache  string // navigation, cache our last location to move back to
	root   bool   // navigation, are we @ root bucket?
}

type item struct {
	Key   string
	Value string
}

type bucket struct {
	Name string
}

func (i *impl) initDB(file string) {
	var err error
	// Connect to KVAL using KVAL default mechanism
	// Can also use regular open plus perms, and kval.Attach()
	i.kb, err = kval.Connect(file)
	if err != nil {
		log.Fatal(err)
	}
}

func (i *impl) updateLoc(bucket string, goBack bool) string {

	// we've probably an invalid value and want to display
	// ourselves again...
	if bucket == i.cache {
		i.loc = bucket
		return i.loc
	}

	// handle goback
	if goBack {
		s := strings.Split(i.loc, ">>")
		i.loc = strings.Join(s[:len(s)-1], ">>")
		i.bucket = strings.Trim(s[len(s)-2], " ")
		return i.loc
	}

	// handle location on merit...
	if i.loc == "" {
		i.loc = bucket
		i.bucket = bucket
	} else {
		i.loc = i.loc + " >> " + bucket
		i.bucket = bucket
	}
	return i.loc
}

func (i *impl) listBucketItems(bucket string, goBack bool) {
	items := []item{}
	getQuery := i.updateLoc(bucket, goBack)
	if getQuery != "" {
		fmt.Fprintf(os.Stdout, "Query: "+getQuery+"\n\n")
		res, err := kval.Query(i.kb, "GET "+getQuery)
		if err != nil {
			if err.Error() == "No Keys: There are no key::value pairs in this bucket" {
				// no values in this bucket
				fmt.Fprintf(os.Stdout, "> There are no key::value pairs in this bucket\n")
				if i.root == true {
					i.listBuckets()
					return
				}
				i.listBucketItems(i.loc, true)
			} else if err.Error() != "Cannot GOTO bucket, bucket not found" {
				log.Fatal(err)
			} else {
				fmt.Fprintf(os.Stdout, "> Bucket not found\n")
				if i.root == true {
					i.listBuckets()
					return
				}
				i.listBucketItems(i.loc, true)
			}
		}
		if len(res.Result) == 0 {
			fmt.Fprintf(os.Stdout, "Invalid request.\n\n")
			i.listBucketItems(i.cache, false)
			return
		}

		for k, v := range res.Result {
			if v == kval.Nestedbucket {
				k = k + bucketSymbol
				v = ""
			}
			items = append(items, item{Key: string(k), Value: string(v)})
		}
		fmt.Fprintf(os.Stdout, "Bucket: %s\n", bucket)
		i.fmt.DumpBucketItems(os.Stdout, i.bucket, items)
		i.root = false     // success this far means we're not at ROOT
		i.cache = getQuery // so we can also set the query cache for paging
		outputInstructionline()
	}
}

func (i *impl) listBuckets() {
	i.root = true
	i.loc = ""

	buckets := []bucket{}

	res, err := kval.Query(i.kb, "GET _") // KVAL: "GET _" will return ROOT
	if err != nil {
		log.Fatal(err)
	}
	for k := range res.Result {
		buckets = append(buckets, bucket{Name: string(k) + bucketSymbol})
	}

	fmt.Fprint(os.Stdout, "DB Layout:\n\n")
	i.fmt.DumpBuckets(os.Stdout, buckets)
	outputInstructionline()
}

func outputInstructionline() {
	fmt.Fprintf(os.Stdout, "\n%s\n\n", instructionLine)
}
