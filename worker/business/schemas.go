package business

import (
	"encoding/csv"
	"middleware/common"
	"reflect"
	"strconv"
	"strings"
)

type Game struct {
	AppID                   string
	Name                    string
	ReleaseDate             string
	EstimatedOwners         string
	PeakCCU                 string
	RequiredAge             string
	Price                   string
	Discount                string
	DLCCount                string
	AboutTheGame            string
	SupportedLanguages      string
	FullAudioLanguages      string
	Reviews                 string
	HeaderImage             string
	Website                 string
	SupportURL              string
	SupportEmail            string
	Windows                 bool
	Mac                     bool
	Linux                   bool
	MetacriticScore         string
	MetacriticURL           string
	UserScore               string
	Positive                string
	Negative                string
	ScoreRank               string
	Achievements            string
	Recommendations         string
	Notes                   string
	AveragePlaytimeForever  float64
	AveragePlaytimeTwoWeeks float64
	MedianPlaytimeForever   float64
	MedianPlaytimeTwoWeeks  float64
	Developers              []string
	Publishers              []string
	Categories              []string
	Genres                  []string
	Tags                    []string
	Screenshots             []string
	Movies                  []string
}

type Review struct {
	AppID       string
	AppName     string
	ReviewText  string
	ReviewScore int
	ReviewVotes int
}

type SOCounter struct {
	Windows uint32
	Linux   uint32
	Mac     uint32
}

func (s *SOCounter) Serialize() []byte {
	se := common.NewSerializer()
	return se.WriteUint32(s.Windows).WriteUint32(s.Linux).WriteUint32(s.Mac).ToBytes()
}

func SOCounterDeserialize(d *common.Deserializer) (*SOCounter, error) {
	windows, err := d.ReadUint32()
	if err != nil {
		return nil, err
	}

	linux, err := d.ReadUint32()
	if err != nil {
		return nil, err
	}
	mac, err := d.ReadUint32()
	if err != nil {
		return nil, err
	}

	return &SOCounter{
		Windows: windows,
		Linux:   linux,
		Mac:     mac,
	}, nil
}

type PlayedTime struct {
	AveragePlaytimeForever float64
	Name                   string
}

func (p *PlayedTime) Serialize() []byte {
	se := common.NewSerializer()
	return se.WriteFloat64(p.AveragePlaytimeForever).WriteString(p.Name).ToBytes()
}

func PlayedTimeDeserialize(d *common.Deserializer) (*PlayedTime, error) {
	pt, err := d.ReadFloat64()
	if err != nil {
		return nil, err
	}
	n, err := d.ReadString()
	if err != nil {
		return nil, err
	}
	return &PlayedTime{
		AveragePlaytimeForever: pt,
		Name:                   n,
	}, nil
}

type GameName struct {
	AppID string
	Name  string
}

func (g *GameName) Serialize() []byte {
	se := common.NewSerializer()
	return se.WriteString(g.AppID).WriteString(g.Name).ToBytes()
}

func GameNameDeserialize(d *common.Deserializer) (*GameName, error) {
	id, err := d.ReadString()
	if err != nil {
		return nil, err
	}

	n, err := d.ReadString()
	if err != nil {
		return nil, err
	}

	return &GameName{
		AppID: id,
		Name:  n,
	}, nil
}

type ValidReview struct {
	AppID string
}

type ReviewCounter struct {
	AppID string
	Count uint32
}

func (c *ReviewCounter) Serialize() []byte {
	se := common.NewSerializer()
	return se.WriteString(c.AppID).WriteUint32(c.Count).ToBytes()
}

func ReviewCounterDeserialize(d *common.Deserializer) (*ReviewCounter, error) {
	id, err := d.ReadString()
	if err != nil {
		return nil, err
	}

	c, err := d.ReadUint32()
	if err != nil {
		return nil, err
	}

	return &ReviewCounter{
		AppID: id,
		Count: c,
	}, nil
}

type NamedReviewCounter struct {
	Name  string
	Count uint32
}

func (c *NamedReviewCounter) Serialize() []byte {
	se := common.NewSerializer()
	return se.WriteString(c.Name).WriteUint32(c.Count).ToBytes()
}

func NamedReviewCounterDeserialize(d *common.Deserializer) (*NamedReviewCounter, error) {
	n, err := d.ReadString()
	if err != nil {
		return nil, err
	}

	c, err := d.ReadUint32()
	if err != nil {
		return nil, err
	}

	return &NamedReviewCounter{
		Name:  n,
		Count: c,
	}, nil
}

func StrParse[T any](s string) (*T, error) {
	var z T
	reader := csv.NewReader(strings.NewReader(s))

	row, err := reader.Read()

	if err != nil {
		return nil, err
	}

	err = mapCSVToStruct(row, &z)

	if err != nil {
		return nil, err
	}

	return &z, nil
}

func mapCSVToStruct(row []string, result interface{}) error {
	v := reflect.ValueOf(result).Elem()

	for i := range row {
		// Just assume that the field are in order
		field := v.Field(i)

		if err := setFieldValue(field, row[i]); err != nil {
			return err
		}
	}
	return nil
}

func setFieldValue(field reflect.Value, value string) error {
	switch field.Kind() {
	case reflect.String:
		field.SetString(value)
	case reflect.Int:
		intValue, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		field.SetInt(int64(intValue))
	case reflect.Float64:
		floatValue, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return err
		}
		field.SetFloat(floatValue)
	case reflect.Bool:
		field.SetBool(value == "true")
	case reflect.Slice:
		field.Set(reflect.ValueOf(parseSlice(value)))
	}
	return nil
}

func parseSlice(s string) []string {
	return strings.Split(strings.Trim(s, `"`), ",")
}