package worker

import (
	"context"
	"encoding/json"
	"log"
	"maps"
	"os"

	"github.com/hibiken/asynq"
	"github.com/jmoiron/sqlx"

	"datacurve-takehome/internal/qa"
	"datacurve-takehome/internal/storage"
)

type Server struct {
	DB    *sqlx.DB
	S3    *storage.Client
	Asynq *asynq.Client
}

func (s *Server) mux() *asynq.ServeMux {
	mux := asynq.NewServeMux()
	mux.HandleFunc("run_full_qa", s.handleQA)
	return mux
}

func (s *Server) handleQA(ctx context.Context, t *asynq.Task) error {
	id := string(t.Payload())
	log.Printf("Starting QA for trace %s", id)

	// get patch from obj storage
	// get obj references from event_batches table
	var object_refs = make([]string, 0)
	err := s.DB.SelectContext(ctx, &object_refs, `select object_ref from event_batches where trace_id=$1`, id)
	if err != nil {
		return err
	}
	log.Printf("found %d objects for trace %s", len(object_refs), id)

	// get from s3
	events := make([]map[string]any, 0)
	for _, ref := range object_refs {
		log.Println("Fetching S3 object:", ref)

		doc, err := s.S3.GetJSON(ctx, ref) // already decoded JSON -> map[string]any
		if err != nil {
			log.Printf("failed to get S3 object %s: %v", ref, err)
			return err
		}

		// Expect {"events": [...]}
		evsAny, ok := doc["events"].([]any)
		if !ok {
			log.Printf("no 'events' array in %s (got keys: %v)", ref, maps.Keys(doc)) // or fmt for Go<1.21
			continue
		}

		for _, e := range evsAny {
			if em, ok := e.(map[string]any); ok {
				events = append(events, em)
			} else {
				log.Printf("skip non-object event in %s: %#v", ref, e)
			}
		}
	}
	// get the patch from events of op "replace", "patch_unified"
	var patch string
	for _, e := range events {
		if e["op"] == "replace" {
			if v, ok := e["patch_unified"].(string); ok {
				patch = v
				break
			}
		}
	}
	log.Println("Using patch:", patch)

	// get the start commit from traces table, commit field in json task column
	var startCommit string
	var repositoryURL string
	var testImage string
	var testCommand string
	var taskJSON []byte
	err = s.DB.GetContext(ctx, &taskJSON, `select task from traces where id=$1`, id)
	if err != nil {
		return err
	}
	var task map[string]any
	if err := json.Unmarshal(taskJSON, &task); err != nil {
		return err
	}
	if c, ok := task["commit"].(string); ok {
		startCommit = c
	}
	if r, ok := task["repository"].(string); ok {
		repositoryURL = r
	}
	if img, ok := task["test_image"].(string); ok {
		testImage = img
	} else {
		testImage = "golang:1.22"
	}
	if cmd, ok := task["test_command"].(string); ok {
		testCommand = cmd
	} else {
		testCommand = "go test ./..."
	}
	log.Println("Using start commit:", startCommit)
	log.Println("Using repository URL:", repositoryURL)

	res, err := qa.RunTests(ctx, repositoryURL, startCommit, patch, testImage, testCommand)
	if err != nil {
		log.Printf("QA runner error: %v", err)
		// persist QA failure detail on the trace instead of panicking
		_, _ = s.DB.ExecContext(ctx,
			`UPDATE traces SET qa = jsonb_set(
            COALESCE(qa, '{}'::jsonb),
            '{error}', to_jsonb($2::text)
         ) WHERE id = $1`,
			id, err.Error(),
		)
		return nil // tell Asynq "done" so it doesn't keep retrying
	}
	qaOut := map[string]any{
		"tests": map[string]any{
			"runner": "docker", "image": testImage, "command": testCommand,
			"ok": res.OK, "stdout": res.Stdout, "stderr": res.Stderr, "exit_code": res.ExitCode,
		},
		"judge": qa.Judge("(summary)", os.Getenv("LLM_MODEL")),
	}
	log.Println("QA result:", qaOut)
	b, _ := json.Marshal(qaOut)
	_, err = s.DB.ExecContext(ctx, `update traces set qa=$1 where id=$2`, b, id)
	return err
}

func Run(addr string, db *sqlx.DB, s3c *storage.Client) error {
	srv := asynq.NewServer(asynq.RedisClientOpt{Addr: addr}, asynq.Config{Concurrency: 5})
	w := &Server{DB: db, S3: s3c}
	return srv.Run(w.mux())
}
