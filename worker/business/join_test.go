package business_test

import (
	"bufio"
	"middleware/common"
	"middleware/worker/business"
	"middleware/worker/schema"
	"os"
	"path/filepath"
	"testing"
)

var gtp = filepath.Join(".", "test_files", "test_1", "join", "1", "game.results")
var rtp = filepath.Join(".", "test_files", "test_1", "join", "1", "review.results")

func deleteFiles() {
	os.Remove(gtp)
	os.Remove(rtp)
}

func recreateFiles() {

	deleteFiles()

	gts, err := common.NewTemporaryStorage(gtp)

	if err != nil {
		panic("Error creating files")
	}
	gts.Append((&schema.GameName{
		AppID: "1",
		Name:  "Test_1",
	}).Serialize())

	gts.Append((&schema.GameName{
		AppID: "3",
		Name:  "Test_3",
	}).Serialize())

	gts.Append((&schema.GameName{
		AppID: "4",
		Name:  "Test_4",
	}).Serialize())

	gts.Close()

	rts, err := common.NewTemporaryStorage(rtp)
	if err != nil {
		panic("Error creating files")
	}
	rts.Append((&schema.ReviewCounter{
		AppID: "1",
		Count: 5,
	}).Serialize())

	rts.Append((&schema.ReviewCounter{
		AppID: "2",
		Count: 50,
	}).Serialize())

	rts.Append((&schema.ReviewCounter{
		AppID: "3",
		Count: 55,
	}).Serialize())

	rts.Append((&schema.ReviewCounter{
		AppID: "4",
		Count: 500,
	}).Serialize())

	rts.Close()
}

func TestJoinOutput(t *testing.T) {
	recreateFiles()
	j, err := business.NewJoin("test_files", "test", "1", 1, 1)
	if err != nil {
		t.Fatal(err)
	}

	expected := map[string]uint32{
		"Test_1": 5,
		"Test_3": 55,
		"Test_4": 500,
	}

	cr, ce := j.NextStage()

loop:
	for {
		select {
		case msg, ok := <-cr:
			if !ok {
				break loop
			}

			v, ok := expected[msg.(*schema.NamedReviewCounter).Name]
			if !ok || v != msg.(*schema.NamedReviewCounter).Count {
				t.Fatal("Unknown")
			}
		case msg, ok := <-ce:
			if !ok {
				break
			}
			t.Fatal(msg)
		}
	}
}

func TestJoinAddReview(t *testing.T) {
	deleteFiles()
	j, err := business.NewJoin("test_files", "test", "1", 1, 1)
	if err != nil {
		t.Fatal(err)
	}

	expected_1 := 6
	expected_2 := 3
	expected_3 := 5

	for i := 0; i < 3; i++ {
		j.AddReview(&schema.ValidReview{
			AppID: "1",
		})
	}

	for i := 0; i < 2; i++ {
		j.AddReview(&schema.ValidReview{
			AppID: "2",
		})
	}

	for i := 0; i < 3; i++ {
		j.AddReview(&schema.ValidReview{
			AppID: "3",
		})
	}

	for i := 0; i < 3; i++ {
		j.AddReview(&schema.ValidReview{
			AppID: "1",
		})
	}

	for i := 0; i < 1; i++ {
		j.AddReview(&schema.ValidReview{
			AppID: "2",
		})
	}

	for i := 0; i < 2; i++ {
		j.AddReview(&schema.ValidReview{
			AppID: "3",
		})
	}

	f, _ := os.Open(rtp)
	s := bufio.NewScanner(f)
	s.Split(func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		raw := data
		if atEOF && len(data) == 0 {
			return 0, nil, nil
		}

		d := common.NewDeserializer(data)
		_, err = schema.ReviewCounterDeserialize(&d)
		if err != nil {
			return 0, nil, nil
		}
		l := len(raw) - d.Buf.Len()
		return l, raw[:l], nil
	})
	s.Buffer(make([]byte, 0, 12), 12)
	i := 0
	for s.Scan() {
		b := s.Bytes()
		d := common.NewDeserializer(b)
		r, _ := schema.ReviewCounterDeserialize(&d)

		if r.AppID == "1" && expected_1 != int(r.Count) {
			t.Fatalf("Wrong Count")
		} else if r.AppID == "2" && expected_2 != int(r.Count) {
			t.Fatalf("Wrong Count")
		} else if r.AppID == "3" && expected_3 != int(r.Count) {
			t.Fatalf("Wrong Count")
		}
		i++
	}
	if i != 3 {
		t.Fatal("Wrong amount of records")
	}
}
