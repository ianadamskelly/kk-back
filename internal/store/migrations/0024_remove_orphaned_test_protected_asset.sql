-- Remove a known orphaned test upload that blocks protected-file reissue.
-- Keep any submitted task text/grade while dropping its missing attachment.

DELETE FROM product_downloads
WHERE url = '/files/20260527-083223-f0d81a78e1ba6e7a5d4b6b67a6b565f6.pdf';

DELETE FROM library_resources
WHERE url = '/files/20260527-083223-f0d81a78e1ba6e7a5d4b6b67a6b565f6.pdf';

DELETE FROM course_resources
WHERE url = '/files/20260527-083223-f0d81a78e1ba6e7a5d4b6b67a6b565f6.pdf';

UPDATE course_task_submissions
SET file_url = ''
WHERE file_url = '/files/20260527-083223-f0d81a78e1ba6e7a5d4b6b67a6b565f6.pdf';
