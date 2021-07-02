CREATE TABLE IF NOT EXISTS songs (
	id bigserial PRIMARY KEY,
	title text NOT NULL,
	artist text NOT NULL,
	year integer NOT NULL
);
