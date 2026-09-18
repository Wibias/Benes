package accountimport

import (
	"context"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/Wibias/Benes/internal/providers/antigravity"
)

type RecordResult struct {
	Index  int    `json:"index"`
	Status string `json:"status"`
	Code   string `json:"code"`
}

type Result struct {
	TotalCount       int            `json:"totalCount"`
	ImportedCount    int            `json:"importedCount"`
	UpdatedCount     int            `json:"updatedCount"`
	FailedCount      int            `json:"failedCount"`
	UnsupportedCount int            `json:"unsupportedCount"`
	Results          []RecordResult `json:"results"`
}

type ServiceOutcome struct {
	OK      bool
	Status  int
	Code    string
	Changed bool
	Result  Result
}

type Validator func(ctx context.Context, refreshToken string) (antigravity.ImportCredential, error)

func Import(ctx context.Context, provider, format string, document any, path string, validate Validator) ServiceOutcome {
	if ctx == nil {
		ctx = context.Background()
	}
	if provider != Provider {
		return ServiceOutcome{Status: 400, Code: "unsupported_provider"}
	}
	if format != Format {
		return ServiceOutcome{Status: 400, Code: "unsupported_format"}
	}
	records, code := ParseCockpitDocument(document)
	if code != "" {
		return ServiceOutcome{Status: 400, Code: code}
	}
	if validate == nil {
		validate = antigravity.ValidateImportCredential
	}
	out := ServiceOutcome{OK: true, Result: Result{Results: []RecordResult{}}}
	for _, item := range records {
		if err := ctx.Err(); err != nil {
			out.OK = false
			out.Status = 408
			out.Code = "import_cancelled"
			out.Changed = out.Result.ImportedCount+out.Result.UpdatedCount > 0
			return out
		}
		if item.Invalid {
			out.Result.Results = append(out.Result.Results, RecordResult{Index: item.Index, Status: "failed", Code: "invalid_record"})
			out.Result.FailedCount++
			continue
		}
		cred, err := validate(ctx, item.RefreshToken)
		if err != nil {
			if ctx.Err() != nil {
				out.OK = false
				out.Status = 408
				out.Code = "import_cancelled"
				out.Changed = out.Result.ImportedCount+out.Result.UpdatedCount > 0
				return out
			}
			out.Result.Results = append(out.Result.Results, RecordResult{Index: item.Index, Status: "failed", Code: "credential_rejected"})
			out.Result.FailedCount++
			continue
		}
		email := safeEmail(cred.Email)
		if email == "" {
			out.Result.Results = append(out.Result.Results, RecordResult{Index: item.Index, Status: "failed", Code: "credential_rejected"})
			out.Result.FailedCount++
			continue
		}
		if email != item.Email {
			out.Result.Results = append(out.Result.Results, RecordResult{Index: item.Index, Status: "failed", Code: "identity_mismatch"})
			out.Result.FailedCount++
			continue
		}
		if strings.TrimSpace(cred.ProjectID) == "" {
			out.Result.Results = append(out.Result.Results, RecordResult{Index: item.Index, Status: "failed", Code: "missing_project"})
			out.Result.FailedCount++
			continue
		}
		if err := ctx.Err(); err != nil {
			out.OK = false
			out.Status = 408
			out.Code = "import_cancelled"
			out.Changed = out.Result.ImportedCount+out.Result.UpdatedCount > 0
			return out
		}
		cred.Email = email
		disposition, err := antigravity.UpsertByIdentity(path, Provider, cred)
		if err != nil {
			out.Result.Results = append(out.Result.Results, RecordResult{Index: item.Index, Status: "failed", Code: "persist_failed"})
			out.Result.FailedCount++
			continue
		}
		status, code := "imported", "imported"
		if disposition == "updated" {
			status, code = "updated", "updated"
			out.Result.UpdatedCount++
		} else {
			out.Result.ImportedCount++
		}
		out.Result.Results = append(out.Result.Results, RecordResult{Index: item.Index, Status: status, Code: code})
		if err := ctx.Err(); err != nil {
			out.OK = false
			out.Status = 408
			out.Code = "import_cancelled"
			out.Changed = true
			return out
		}
	}
	out.Result.TotalCount = len(out.Result.Results)
	return out
}

var emailControl = regexp.MustCompile(`[\x00-\x20\x7f]`)

func safeEmail(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" || utf8.RuneCountInString(normalized) > MaxEmailLength || emailControl.MatchString(normalized) {
		return ""
	}
	return normalized
}
