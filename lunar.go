package lunar

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

/*
cd ./files && curl -O https://www.hko.gov.hk/tc/gts/time/calendar/text/files/T\[1901-2100\]c.txt && \
	find . -type f -exec sh -c 'iconv -f big5 -t utf-8 -c {} > {}.utf8' \; -exec mv "{}".utf8 "{}" \; && cd ..
*/

var (
	ErrNotFound  = errors.New("lunar: date not found")
	loadFileFunc func(string) (io.ReadCloser, error)
)

type Result struct {
	Date      Date
	LunarDate Date
	Weekday   time.Weekday
	SolarTerm string
}

type Date struct {
	Year  int
	Month int
	Day   int
}

func DateByTime(t time.Time) Date {
	year, month, day := t.Date()
	return Date{
		Year:  year,
		Month: int(month),
		Day:   day,
	}
}

func (d Date) Time() time.Time {
	return time.Date(d.Year, time.Month(d.Month), d.Day, 0, 0, 0, 0, time.UTC)
}

func (d Date) Equal(d1 Date) bool {
	return d.Year == d1.Year && d.Month == d1.Month && d.Day == d1.Day
}

func (d Date) String() string {
	return d.Time().Format("20060102")
}

func (d Date) Valid() bool {
	return d.Year != 0 && d.Month != 0 && d.Day != 0
}

func fileDateFormat(year int) string {
	format := "2006年1月2日"
	if year <= 2010 {
		format = "2006年01月02日"
	}

	return format
}

var lunarMap = map[rune]int{
	'天': 0,
	'初': 0,
	'正': 1,
	'一': 1,
	'二': 2,
	'廿': 2,
	'三': 3,
	'四': 4,
	'五': 5,
	'六': 6,
	'七': 7,
	'八': 8,
	'九': 9,
	'十': 10,
}

var defaultHandler = New()

type Handler struct {
	cacheEnabled   bool
	dateCache      map[int]map[Date]*Result
	lunarDateCache map[int]map[Date]*Result
}

func New() *Handler {
	return &Handler{
		dateCache:      map[int]map[Date]*Result{},
		lunarDateCache: map[int]map[Date]*Result{},
	}
}

func DateToLunarDate(d Date) (*Result, error) {
	return defaultHandler.DateToLunarDate(d)
}

func (h *Handler) DateToLunarDate(d Date) (*Result, error) {
	f, err := loadFileFunc(fmt.Sprintf("T%dc.txt", d.Year))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	lunarMonth := 0
	return h.find(f, d, true, d.Year, d.Year-1, &lunarMonth)
}

func LunarDateToDate(d Date) (*Result, error) {
	return defaultHandler.LunarDateToDate(d)
}

func (h *Handler) LunarDateToDate(d Date) (*Result, error) {
	f, err := loadFileFunc(fmt.Sprintf("T%dc.txt", d.Year))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	lunarMonth := 0
	r, err := h.find(f, d, false, d.Year, d.Year-1, &lunarMonth)
	if err == nil {
		return r, nil
	}
	if err != ErrNotFound {
		return nil, err
	}

	f1, err := loadFileFunc(fmt.Sprintf("T%dc.txt", d.Year+1))
	if err != nil {
		return nil, err
	}
	defer f1.Close()

	return h.find(f1, d, false, d.Year+1, d.Year, &lunarMonth)
}

func (h *Handler) find(rd io.Reader, d Date, dateToLunarDate bool, fileYear, lunarYear int, lunarMonth *int) (*Result, error) {
	r, err := prepareReader(rd)
	if err != nil {
		return nil, err
	}

	var result *Result
	unknownMonthResults := []*Result{}
	for {
		line, err := r.ReadString('\n')
		if len(line) == 0 && err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}

		res, newunknownMonthResults, err := h.parseLine(line, fileYear, lunarYear, *lunarMonth, unknownMonthResults)
		if res == nil && err == nil {
			continue
		}

		if err != nil {
			return nil, err
		}
		lunarYear, *lunarMonth = res.LunarDate.Year, res.LunarDate.Month

		if dateToLunarDate {
			if res.Date.Equal(d) {
				result = res
			}
		} else {
			if res.LunarDate.Equal(d) {
				result = res
			}

			if len(unknownMonthResults) > 0 && len(newunknownMonthResults) == 0 {
				for _, v := range unknownMonthResults {
					if v.LunarDate.Equal(d) {
						result = res
					}
				}
			}
		}

		if result != nil && result.LunarDate.Valid() && !h.cacheEnabled {
			return result, nil
		}
	}

	if result == nil || !result.LunarDate.Valid() {
		return nil, ErrNotFound
	}

	return result, nil
}

func (h *Handler) parseLine(line string, fileYear int, lunarYear, lunarMonth int, unknownMonthResults []*Result) (*Result, []*Result, error) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return nil, nil, nil
	}

	rs := []rune(fields[1])
	if rs[0] == rune('閏') {
		rs = rs[1:]
	}

	isMonth := false
	if rs[len(rs)-1] == rune('月') {
		isMonth = true
		rs = rs[:len(rs)-1]
	}

	lastChar := rs[len(rs)-1]
	unitDigit := lunarMap[lastChar]
	if lastChar == '正' {
		lunarYear++
	}

	tensDigit := 0
	if len(rs) > 1 {
		tensDigit = lunarMap[rs[0]]
		if tensDigit == 10 {
			tensDigit = 1
		}
		if tensDigit != 0 && unitDigit == 10 {
			tensDigit--
		}
	}

	lunarDay := tensDigit*10 + unitDigit
	if isMonth {
		lunarMonth = lunarDay
		lunarDay = 1
	}

	newunknownMonthResults := unknownMonthResults
	if isMonth && len(unknownMonthResults) > 0 {
		tmpLunarMonth := lunarMonth - 1
		if tmpLunarMonth == 0 {
			tmpLunarMonth = 12
		}

		for _, v := range unknownMonthResults {
			v.LunarDate.Month = tmpLunarMonth
			h.cache(v, fileYear)
		}
		newunknownMonthResults = []*Result{}
	}

	t, err := time.Parse(fileDateFormat(fileYear), fields[0])
	if err != nil {
		return nil, nil, fmt.Errorf("lunar: parse time error: %w", err)
	}

	weekday := []rune(fields[2])
	r := &Result{
		Date:      DateByTime(t),
		LunarDate: Date{lunarYear, lunarMonth, lunarDay},
		Weekday:   time.Weekday(lunarMap[weekday[len(weekday)-1]]),
	}
	if len(fields) > 3 {
		r.SolarTerm = fields[3]
	}

	if lunarMonth == 0 {
		newunknownMonthResults = append(unknownMonthResults, r)
	} else {
		h.cache(r, fileYear)
	}

	return r, newunknownMonthResults, nil
}

func (h *Handler) cache(r *Result, fileYear int) {
	if h.cacheEnabled {
		yearDateMap, ok := h.dateCache[fileYear]
		if !ok {
			yearDateMap = map[Date]*Result{}
			h.dateCache[fileYear] = yearDateMap
		}

		yearLunarDateMap, ok := h.lunarDateCache[fileYear]
		if !ok {
			yearLunarDateMap = map[Date]*Result{}
			h.lunarDateCache[fileYear] = yearLunarDateMap
		}

		yearDateMap[r.Date] = r
		yearLunarDateMap[r.LunarDate] = r
	}
}

func prepareReader(rd io.Reader) (*bufio.Reader, error) {
	r := bufio.NewReader(rd)

	// skip first three lines
	for i := 0; i < 3; i++ {
		line, err := r.ReadString('\n')
		if len(line) == 0 && err != nil {
			return nil, err
		}
	}

	return r, nil
}
