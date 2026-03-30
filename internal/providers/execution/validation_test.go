package execution

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONValidator_Valid(t *testing.T) {
	v := JSONValidator{}
	assert.Equal(t, "json", v.Name())
	assert.NoError(t, v.Validate(`{"key": "value"}`))
	assert.NoError(t, v.Validate(`[1, 2, 3]`))
	assert.NoError(t, v.Validate(`"just a string"`))
	assert.NoError(t, v.Validate(`  {"key": "value"}  `))
}

func TestJSONValidator_Invalid(t *testing.T) {
	v := JSONValidator{}
	assert.Error(t, v.Validate(`not json`))
	assert.Error(t, v.Validate(`{broken`))
	assert.Error(t, v.Validate(``))
}

func TestNonEmptyValidator_Valid(t *testing.T) {
	v := NonEmptyValidator{}
	assert.Equal(t, "non_empty", v.Name())
	assert.NoError(t, v.Validate("hello"))
	assert.NoError(t, v.Validate("  hello  "))
}

func TestNonEmptyValidator_Invalid(t *testing.T) {
	v := NonEmptyValidator{}
	assert.Error(t, v.Validate(""))
	assert.Error(t, v.Validate("   "))
	assert.Error(t, v.Validate("\n\t"))
}

func TestMinLengthValidator_Valid(t *testing.T) {
	v := MinLengthValidator{Min: 5}
	assert.Equal(t, "min_length", v.Name())
	assert.NoError(t, v.Validate("hello"))
	assert.NoError(t, v.Validate("hello world"))
}

func TestMinLengthValidator_Invalid(t *testing.T) {
	v := MinLengthValidator{Min: 10}
	assert.Error(t, v.Validate("hi"))
	assert.Error(t, v.Validate(""))
}

func TestValidationPolicy_NoValidators(t *testing.T) {
	p := DefaultValidationPolicy()
	assert.NoError(t, p.Run("anything"))
	assert.NoError(t, p.Run(""))
}

func TestValidationPolicy_SingleValidator(t *testing.T) {
	p := ValidationPolicy{
		Validators: []Validator{JSONValidator{}},
		FailAction: ActionNextCandidate,
	}

	assert.NoError(t, p.Run(`{"key": "value"}`))

	err := p.Run("not json")
	require.Error(t, err)

	var ve *ValidationError
	require.ErrorAs(t, err, &ve)
	assert.Contains(t, ve.Reason, "json")
}

func TestValidationPolicy_MultipleValidators(t *testing.T) {
	p := ValidationPolicy{
		Validators: []Validator{
			NonEmptyValidator{},
			JSONValidator{},
		},
		FailAction: ActionNextCandidate,
	}

	assert.NoError(t, p.Run(`{"ok": true}`))

	err := p.Run("")
	require.Error(t, err)
	var ve *ValidationError
	require.ErrorAs(t, err, &ve)
	assert.Contains(t, ve.Reason, "non_empty")
}

func TestValidationPolicy_StopsOnFirstError(t *testing.T) {
	p := ValidationPolicy{
		Validators: []Validator{
			NonEmptyValidator{},
			MinLengthValidator{Min: 100},
			JSONValidator{},
		},
		FailAction: ActionAbort,
	}

	err := p.Run("short")
	require.Error(t, err)
	var ve *ValidationError
	require.ErrorAs(t, err, &ve)
	assert.Contains(t, ve.Reason, "min_length")
}
