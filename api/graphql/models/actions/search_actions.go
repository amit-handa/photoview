package actions

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/photoview/photoview/api/database/drivers"
	"github.com/photoview/photoview/api/graphql/models"
	"github.com/pkg/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type searchQuery struct {
	Attributes []string
	query      string
}

func parseQuery(query string) searchQuery {
	regex, _ := regexp.Compile("a:\\S+")
	matches := regex.FindAllStringIndex(query, -1)

	var attrs []string
	var queryBuilder strings.Builder
	var i = 0
	for _, m := range matches {
		queryBuilder.WriteString(query[i:m[0]])
		attrs = append(attrs, query[m[0]+2:m[1]])
		i = m[1]
	}

	return searchQuery{Attributes: attrs, query: strings.TrimSpace(queryBuilder.String())}
}

func Search(db *gorm.DB, query string, userID int, _limitMedia *int, _limitAlbums *int) (*models.SearchResult, error) {
	limitMedia := 10
	limitAlbums := 10

	if _limitMedia != nil {
		limitMedia = *_limitMedia
	}

	if _limitAlbums != nil {
		limitAlbums = *_limitAlbums
	}

	pquery := parseQuery(query)
	query = pquery.query

	wildQuery := "%" + strings.ToLower(query) + "%"

	var media []*models.Media

	userSubquery := db.Table("user_albums").Where("user_id = ?", userID)
	if drivers.POSTGRES.MatchDatabase(db) {
		userSubquery = userSubquery.Where("album_id = \"Album\".id")
	} else {
		userSubquery = userSubquery.Where("album_id = Album.id")
	}

	q := db.Joins("Album").Joins("Exif").
		Where("EXISTS (?)", userSubquery).
		Where("LOWER(media.title) LIKE ? OR LOWER(media.path) LIKE ?", wildQuery, wildQuery).
		Clauses(clause.OrderBy{
			Expression: clause.Expr{
				SQL:                "(CASE WHEN LOWER(media.title) LIKE ? THEN 2 WHEN LOWER(media.path) LIKE ? THEN 1 END) DESC",
				Vars:               []interface{}{wildQuery, wildQuery},
				WithoutParentheses: true},
		}).
		Limit(limitMedia)

	for _, attr := range pquery.Attributes {
		q = q.Where("json_contains(Exif.attributes, ?)", fmt.Sprintf("\"%s\"", attr))
	}

	err := q.Find(&media).Error

	if err != nil {
		return nil, errors.Wrapf(err, "searching media")
	}

	var albums []*models.Album

	err = db.
		Where("EXISTS (?)", db.Table("user_albums").Where("user_id = ?", userID).Where("album_id = albums.id")).
		Where("albums.title LIKE ? OR albums.path LIKE ?", wildQuery, wildQuery).
		Clauses(clause.OrderBy{
			Expression: clause.Expr{
				SQL:                "(CASE WHEN albums.title LIKE ? THEN 2 WHEN albums.path LIKE ? THEN 1 END) DESC",
				Vars:               []interface{}{wildQuery, wildQuery},
				WithoutParentheses: true},
		}).
		Limit(limitAlbums).
		Find(&albums).Error

	if err != nil {
		return nil, errors.Wrapf(err, "searching albums")
	}

	result := models.SearchResult{
		Query:  query,
		Media:  media,
		Albums: albums,
	}

	return &result, nil
}
