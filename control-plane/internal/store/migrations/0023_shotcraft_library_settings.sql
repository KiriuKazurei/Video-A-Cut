CREATE TABLE shotcraft_library_settings (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    directory TEXT NOT NULL
);
