CREATE UNIQUE INDEX IF NOT EXISTS retrieval_results_request_rank_unique
    ON retrieval_results (request_id, rank);
