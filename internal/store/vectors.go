package store

import (
	"lossless/internal/embed"
	"lossless/internal/projectkey"
)

type VecHit struct {
	ID     string
	Cosine float32
}

func (s *Store) PutVector(recordID, project, model string, vec []float32) error {
	if recordID == "" || model == "" || len(vec) == 0 {
		return nil
	}
	project = projectkey.Normalize(project)
	_, err := s.DB.Exec(`
INSERT INTO claim_vectors(record_id, project_key, model, dim, vec)
VALUES(?,?,?,?,?)
ON CONFLICT(record_id) DO UPDATE SET
  project_key=excluded.project_key, model=excluded.model, dim=excluded.dim, vec=excluded.vec`,
		recordID, project, model, len(vec), embed.Encode(vec))
	return err
}

func (s *Store) GetVector(recordID string) ([]float32, string, error) {
	var blob []byte
	var model string
	err := s.DB.QueryRow(`SELECT vec, model FROM claim_vectors WHERE record_id = ?`, recordID).Scan(&blob, &model)
	if err != nil {
		return nil, "", err
	}
	return embed.Decode(blob), model, nil
}

func (s *Store) DeleteVector(recordID string) error {
	_, err := s.DB.Exec(`DELETE FROM claim_vectors WHERE record_id = ?`, recordID)
	return err
}

func (s *Store) SearchKNN(project, model string, query []float32, limit int) ([]VecHit, error) {
	if limit <= 0 || len(query) == 0 || model == "" {
		return nil, nil
	}
	project = projectkey.Normalize(project)
	rows, err := s.DB.Query(`
SELECT v.record_id, v.vec
FROM claim_vectors v
JOIN records r ON r.id = v.record_id
WHERE v.project_key = ? AND v.model = ? AND v.dim = ? AND r.status = 'active'`,
		project, model, len(query))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var best []VecHit
	for rows.Next() {
		var id string
		var blob []byte
		if err := rows.Scan(&id, &blob); err != nil {
			return nil, err
		}
		vec := embed.Decode(blob)
		cos := embed.Cosine(query, vec)
		if cos <= 0 {
			continue
		}
		best = insertVec(best, VecHit{ID: id, Cosine: cos}, limit)
	}
	return best, rows.Err()
}

func insertVec(best []VecHit, h VecHit, limit int) []VecHit {
	i := 0
	for i < len(best) && best[i].Cosine >= h.Cosine {
		i++
	}
	best = append(best, VecHit{})
	copy(best[i+1:], best[i:])
	best[i] = h
	if len(best) > limit {
		best = best[:limit]
	}
	return best
}

func (s *Store) embedClaim(id, project, text string, symbols []string) {
	if s.Embedder == nil || id == "" {
		return
	}
	doc := embed.Document(text, symbols)
	vecs, err := s.Embedder.Embed([]string{doc})
	if err != nil || len(vecs) == 0 || len(vecs[0]) == 0 {
		return
	}
	embed.Normalize(vecs[0])
	_ = s.PutVector(id, project, s.Embedder.Name(), vecs[0])
}

func (s *Store) VectorCount() int {
	var n int
	_ = s.DB.QueryRow(`SELECT count(*) FROM claim_vectors`).Scan(&n)
	return n
}

func (s *Store) EmbedderName() string {
	if s == nil || s.Embedder == nil {
		return ""
	}
	return s.Embedder.Name()
}

func AttachEmbedder(st *Store, home string) {
	if st == nil || st.Embedder != nil {
		return
	}
	st.Embedder = embed.Open(home)
}

func (s *Store) BackfillVectors() (int, error) {
	if s.Embedder == nil {
		return 0, nil
	}
	rows, err := s.DB.Query(`SELECT ` + claimCols + ` FROM records WHERE status = 'active'`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		rec, err := scanRow(rows)
		if err != nil {
			return n, err
		}
		vec, model, err := s.GetVector(rec.ID)
		if err == nil && model == s.Embedder.Name() && len(vec) == s.Embedder.Dim() {
			continue
		}
		s.embedClaim(rec.ID, rec.ProjectKey, rec.Text, rec.Symbols)
		n++
	}
	return n, rows.Err()
}
