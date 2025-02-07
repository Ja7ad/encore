package srcerrors

import (
	"fmt"
	"go/ast"
	"go/scanner"
	"go/token"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"

	"encr.dev/pkg/errinsrc"
	. "encr.dev/pkg/errinsrc/internal"
	"encr.dev/pkg/idents"
	schema "encr.dev/proto/encore/parser/schema/v1"
)

// UnhandledPanic is an error we use to wrap a panic that was not handled
// It should ideally never be seen by users, but if it is, it means we have
// a bug within Encore which needs fixing.
func UnhandledPanic(recovered any) error {
	if err := errinsrc.ExtractFromPanic(recovered); err != nil {
		return err
	}

	// If recovered is an error, then track it as the source
	var srcError error
	if err, ok := recovered.(error); ok {
		srcError = err
	}
	// If we get here, it's an unhandled panic / error
	return errinsrc.New(ErrParams{
		Code:    1,
		Title:   "Unhandled Panic",
		Summary: fmt.Sprintf("A unhandled panic occurred: %v", recovered),
		Detail:  internalErrReportToEncore,
		Cause:   srcError,
	}, true)
}

// GenericGoParserError reports an error was that was reported from the Go parser.
// It should not be returned by any errors caused by Encore's own parser as they
// should have specific errors listed below
func GenericGoParserError(err *scanner.Error) *errinsrc.ErrInSrc {
	locs := SrcLocations{}
	if pos := FromGoTokenPositions(err.Pos, err.Pos); pos != nil {
		locs = SrcLocations{pos}
	}

	return errinsrc.New(ErrParams{
		Code:      2,
		Title:     "Parse Error in Go Source",
		Summary:   err.Msg,
		Cause:     err,
		Locations: locs,
	}, false)
}

// GenericGoPackageError reports an error was that was reported from the Go package loader.
// It should not be returned by any errors caused by Encore's own parser as they
// should have specific errors listed below
func GenericGoPackageError(err packages.Error) *errinsrc.ErrInSrc {
	var locations SrcLocations

	// Extract the position from the error
	var pos token.Position
	switch p := strings.SplitN(err.Pos, ":", 3); len(p) {
	case 3:
		pos.Column, _ = strconv.Atoi(p[2])
		fallthrough
	case 2:
		pos.Line, _ = strconv.Atoi(p[1])
		fallthrough
	case 1:
		if p[0] != "" && p[0] != "-" {
			pos.Filename = p[0]
		}
	}
	if pos.Filename != "" && pos.Line > 0 {
		locations = SrcLocations{FromGoTokenPositions(pos, pos)}
	}

	return errinsrc.New(ErrParams{
		Code:      3,
		Title:     "Go Package Error",
		Summary:   err.Msg,
		Cause:     err,
		Locations: locations,
	}, false)
}

// GenericGoCompilerError reports an error was that was reported from the Go compiler.
// It should not be returned by any errors caused by Encore's own compiler as they
// should have specific errors listed below.
func GenericGoCompilerError(fileName string, lineNumber int, column int, error string) error {
	errLocation := token.Position{
		Filename: fileName,
		Offset:   0,
		Line:     lineNumber,
		Column:   column,
	}

	return errinsrc.New(ErrParams{
		Code:      3,
		Title:     "Go Compilation Error",
		Summary:   strings.TrimSpace(error),
		Locations: SrcLocations{FromGoTokenPositions(errLocation, errLocation)},
	}, false)
}

// StandardLibraryError is an error that is not caused by Encore, but is
// returned by a standard library function. We wrap it in an ErrInSrc so that
// we can still possibly provide a source location.
func StandardLibraryError(err error) *errinsrc.ErrInSrc {
	return errinsrc.New(ErrParams{
		Code:    3,
		Title:   "Error",
		Summary: err.Error(),
		Cause:   err,
	}, true)
}

// GenericError is a place holder for errors reported through perr.Add or perr.Addf
func GenericError(pos token.Position, msg string) *errinsrc.ErrInSrc {
	return errinsrc.New(ErrParams{
		Code:      3,
		Title:     "Error",
		Summary:   msg,
		Locations: SrcLocations{FromGoTokenPositions(pos, pos)},
	}, false)
}

func DatabaseNotFound(fileset *token.FileSet, node ast.Node, dbName string) error {
	return errinsrc.New(ErrParams{
		Code:      4,
		Title:     "Database Not Found",
		Summary:   fmt.Sprintf("The database %s was not found", dbName),
		Locations: SrcLocations{FromGoASTNode(fileset, node)},
	}, false)
}

func UnknownErrorCompilingConfig(fileset *token.FileSet, node ast.Node, err error) error {
	return errinsrc.New(ErrParams{
		Code:      5,
		Title:     "Error compiling configuration",
		Summary:   err.Error(),
		Cause:     err,
		Locations: SrcLocations{FromGoASTNode(fileset, node)},
	}, false)
}

func UnableToLoadCUEInstances(err error, pathPrefix string) error {
	return handleCUEError(err, pathPrefix, ErrParams{
		Code:  6,
		Title: "Unable to load CUE instances",
	})
}

func UnableToAddOrphanedCUEFiles(err error, pathPrefix string) error {
	return handleCUEError(err, pathPrefix, ErrParams{
		Code:  7,
		Title: "Unable to add orphaned CUE files",
	})
}

func CUEEvaluationFailed(err error, pathPrefix string) error {
	return handleCUEError(err, pathPrefix, ErrParams{
		Code:  8,
		Title: "CUE evaluation failed",
		Detail: "While evaluating the CUE configuration to generate a concrete configuration for your application, CUE returned an error. " +
			"This is usually caused by either a constraint on a field being unsatisfied or there being two different values for a given field. " +
			"For more information on CUE and this error, see https://cuelang.org/docs/",
	})
}

func ConfigOnlyLoadedFromService(fileset *token.FileSet, node ast.Node) error {
	return errinsrc.New(ErrParams{
		Code:      9,
		Title:     "Invalid call to config.Load[T]()",
		Summary:   "A call to config.Load[T]() can only be made from within a service.",
		Detail:    combine(makeService, configHelp),
		Locations: SrcLocations{FromGoASTNode(fileset, node)},
	}, false)
}

func ConfigMustBeTopLevelPackage(fileset *token.FileSet, node ast.Node) error {
	return errinsrc.New(ErrParams{
		Code:      10,
		Title:     "Invalid call to config.Load[T]()",
		Summary:   "A call to config.Load[T]() can only be made from the top level package of a service.",
		Detail:    configHelp,
		Locations: SrcLocations{FromGoASTNode(fileset, node)},
	}, false)
}

func ConfigLoadNoArguments(fileset *token.FileSet, node *ast.CallExpr) error {
	start := fileset.Position(node.Lparen + 1)
	end := fileset.Position(node.Rparen)

	return errinsrc.New(ErrParams{
		Code:      11,
		Title:     "Invalid call to config.Load[T]()",
		Summary:   "A call to config.Load[T]() does not accept any arguments.",
		Detail:    configHelp,
		Locations: SrcLocations{FromGoTokenPositions(start, end)},
	}, false)
}

func ConfigOnlyReferencedSameService(fileset *token.FileSet, reference ast.Node, defined ast.Node) error {
	refLoc := FromGoASTNode(fileset, reference)
	refLoc.Text = "referenced here"

	definedLoc := FromGoASTNode(fileset, defined)
	definedLoc.Type = LocHelp
	definedLoc.Text = "defined here"

	return errinsrc.New(ErrParams{
		Code:      12,
		Title:     "Cross service resource reference",
		Summary:   "A config instance can only be referenced from within the service that the call to `config.Load[T]()` was made in.",
		Detail:    configHelp,
		Locations: SrcLocations{refLoc, definedLoc},
	}, false)
}

func UnknownConfigWrapperType(fileset *token.FileSet, node ast.Node, ident *ast.Ident) error {
	return errinsrc.New(ErrParams{
		Code:      13,
		Title:     "Unknown config type",
		Summary:   fmt.Sprintf("config.%s is not type which can be used within data structures", ident.Name),
		Detail:    configHelp,
		Locations: SrcLocations{FromGoASTNode(fileset, node)},
	}, false)
}

func ConfigValueTypeNotSet(fileset *token.FileSet, node ast.Node) error {
	return errinsrc.New(ErrParams{
		Code:      14,
		Title:     "Internal Error",
		Summary:   "The type of a config value was not set.",
		Detail:    internalErrReportToEncore,
		Locations: SrcLocations{FromGoASTNode(fileset, node)},
	}, true)
}

func ConfigWrapperNested(fileset *token.FileSet, node ast.Node, funcCall *ast.CallExpr) error {
	loc := FromGoASTNode(fileset, funcCall)
	loc.Text = "loaded from here"

	locs := SrcLocations{loc}

	if node != nil {
		loc.Type = LocHelp

		field := FromGoASTNode(fileset, node)
		locs = SrcLocations{field, loc}
	}

	return errinsrc.New(ErrParams{
		Code:      15,
		Title:     "Invalid config type",
		Summary:   "The type of config.Value[T] cannot be another config.Value[T]",
		Detail:    configHelp,
		Locations: locs,
	}, false)
}

func ConfigTypeHasUnexportFields(fileset *token.FileSet, loadCall ast.Node, field *ast.Field) error {
	loadLoc := FromGoASTNode(fileset, loadCall)
	loadLoc.Text = "loaded from here"
	loadLoc.Type = LocHelp

	return errinsrc.New(ErrParams{
		Code:      16,
		Title:     "Invalid config type",
		Summary:   fmt.Sprintf("Field %s is not exported and is in a datatype which is used by a call to `config.Load[T]()`. Unexported fields cannot be initialised by Encore, thus are not allowed in this context.", field.Names[0].Name),
		Detail:    configHelp,
		Locations: SrcLocations{FromGoASTNode(fileset, field), loadLoc},
	}, false)
}

func ResourceNameNotStringLiteral(fileset *token.FileSet, node ast.Node, resourceType string, paramName string) error {
	return errinsrc.New(ErrParams{
		Code:      17,
		Title:     "Invalid resource name",
		Summary:   fmt.Sprintf("A %s requires the %s given as a string literal.", resourceType, paramName),
		Detail:    resourceNameHelpKebabCase(resourceType, paramName),
		Locations: SrcLocations{FromGoASTNodeWithTypeAndText(fileset, node, LocError, fmt.Sprintf("was given %s", nodeType(node)))},
	}, false)
}

func ResourceNameWrongLength(fileset *token.FileSet, node ast.Node, resourceType string, paramName string, name string) error {
	return errinsrc.New(ErrParams{
		Code:      18,
		Title:     "Invalid resource name",
		Summary:   fmt.Sprintf("The %s %s needs to be between 1 and 63 characters long.", resourceType, paramName),
		Detail:    resourceNameHelpKebabCase(resourceType, paramName),
		Locations: SrcLocations{FromGoASTNodeWithTypeAndText(fileset, node, LocError, fmt.Sprintf("is %d characters long", len(name)))},
	}, false)
}

func ResourceNameNotKebabCase(fileset *token.FileSet, node ast.Node, resourceType string, paramName string, name string) error {
	proposedName := idents.GenerateSuggestion(name, idents.KebabCase)

	return errinsrc.New(ErrParams{
		Code:      19,
		Title:     "Invalid resource name",
		Summary:   fmt.Sprintf("The %s must be %s be defined in \"kebab-case\"", resourceType, paramName),
		Detail:    resourceNameHelpKebabCase(resourceType, paramName),
		Locations: SrcLocations{FromGoASTNodeWithTypeAndText(fileset, node, LocError, fmt.Sprintf("try \"%s\"?", proposedName))},
	}, false)
}

func PubSubNewTopicInvalidArgCount(fileset *token.FileSet, node *ast.CallExpr) error {
	start := fileset.Position(node.Lparen + 1)
	end := fileset.Position(node.Rparen)

	return errinsrc.New(ErrParams{
		Code:    20,
		Title:   "Invalid call to pubsub.NewTopic",
		Summary: "A call to pubsub.NewTopic requires two arguments, the topic name and the topic configuration",
		Detail: combine(
			pubsubNewTopicHelp,
			pubsubHelp,
		),
		Locations: SrcLocations{FromGoTokenPositions(start, end)},
	}, false)
}

func PubSubTopicNameNotUnique(fileset *token.FileSet, firstDefinition ast.Node, secondDefinition ast.Node) error {
	first := FromGoASTNode(fileset, firstDefinition)
	second := FromGoASTNode(fileset, secondDefinition)

	first.Text = "originally defined here"
	first.Type = LocHelp

	second.Text = "redefined here"

	return errinsrc.New(ErrParams{
		Code:    21,
		Title:   "Duplicate PubSub topic name",
		Summary: "A PubSub topic name must be unique within an application.",
		Detail: combine(
			resourceNameHelpKebabCase("pub sub topic", "name"),
			"If you wish to reuse the same topic, then you can export the original Topic object import it here.",
			pubsubHelp,
		),
		Locations: SrcLocations{first, second},
	}, false)
}

func PubSubTopicConfigNotConstant(fileset *token.FileSet, fieldName string, node ast.Node) error {
	return errinsrc.New(ErrParams{
		Code:    22,
		Title:   "Invalid PubSub topic config",
		Summary: fmt.Sprintf("All values in pubsub.TopicConfig must be a constant, however %s was not a constant.", fieldName),
		Detail: combine(
			pubsubNewTopicHelp,
			pubsubHelp,
		),
		Locations: SrcLocations{FromGoASTNodeWithTypeAndText(fileset, node, LocError, "got "+nodeType(node))},
	}, false)
}

func PubSubTopicConfigMissingField(fileset *token.FileSet, fieldName string, node ast.Node) error {
	return errinsrc.New(ErrParams{
		Code:    23,
		Title:   "Invalid PubSub topic config",
		Summary: fmt.Sprintf("pubsub.NewTopic requires the configuration field named \"%s\" to be explicitly set.", fieldName),
		Detail: combine(
			pubsubNewTopicHelp,
			pubsubHelp,
		),
		Locations: SrcLocations{FromGoASTNodeWithTypeAndText(fileset, node, LocError, "got "+nodeType(node))},
	}, false)
}

func PubSubTopicConfigInvalidField(fileset *token.FileSet, fieldName string, exampleValue string, node ast.Node) error {
	return errinsrc.New(ErrParams{
		Code:    24,
		Title:   "Invalid PubSub topic config",
		Summary: fmt.Sprintf("pubsub.NewTopic requires the configuration field named \"%s\" to be a valid value", fieldName),
		Detail: combine(
			pubsubNewTopicHelp,
			pubsubHelp,
		),
		Locations: SrcLocations{FromGoASTNodeWithTypeAndText(fileset, node, LocError, "try %s "+exampleValue)},
	}, false)
}

func PubSubOrderingKeyMustBeExported(fileset *token.FileSet, node ast.Node) error {
	return errinsrc.New(ErrParams{
		Code:      25,
		Title:     "Invalid PubSub topic config",
		Summary:   "The configuration field named \"OrderingKey\" must be a one of the exported fields on the message type.",
		Detail:    pubsubHelp,
		Locations: SrcLocations{FromGoASTNode(fileset, node)},
	}, false)
}

func PubSubOrderingKeyNotStringLiteral(fileset *token.FileSet, node ast.Node) error {
	return errinsrc.New(ErrParams{
		Code:      26,
		Title:     "Invalid PubSub topic config",
		Summary:   "pubsub.NewTopic requires the configuration field named \"OrderingKey\" to either not be set, or be set to a non empty string referencing the field in the message type you want to order messages by.",
		Detail:    pubsubHelp,
		Locations: SrcLocations{FromGoASTNode(fileset, node)},
	}, false)
}

func PubSubSubscriptionArguments(fileset *token.FileSet, node *ast.CallExpr) error {
	start := fileset.Position(node.Lparen + 1)
	end := fileset.Position(node.Rparen)

	return errinsrc.New(ErrParams{
		Code:    27,
		Title:   "Invalid call to pubsub.NewSubscription",
		Summary: "A call to pubsub.NewSubscription requires three arguments, the topic, the subscription name given as a string literal and the subscription configuration",
		Detail: combine(
			pubsubNewSubscriptionHelp,
			pubsubHelp,
		),
		Locations: SrcLocations{FromGoTokenPositions(start, end)},
	}, false)
}

func PubSubSubscriptionTopicNotResource(fileset *token.FileSet, node ast.Expr, got string) error {
	if got == "" {
		got = "got " + nodeType(node)
	}

	return errinsrc.New(ErrParams{
		Code:    28,
		Title:   "Invalid call to pubsub.NewSubscription",
		Summary: "pubsub.NewSubscription requires the first argument to reference to pubsub topic",
		Detail: combine(
			pubsubNewSubscriptionHelp,
			pubsubHelp,
		),
		Locations: SrcLocations{FromGoASTNodeWithTypeAndText(fileset, node, LocError, got)},
	}, false)
}

func PubSubSubscriptionNameNotUnique(fileset *token.FileSet, firstDefinition ast.Node, secondDefinition ast.Node) error {
	first := FromGoASTNode(fileset, firstDefinition)
	second := FromGoASTNode(fileset, secondDefinition)

	first.Text = "originally defined here"
	first.Type = LocHelp

	second.Text = "redefined here"

	return errinsrc.New(ErrParams{
		Code:    29,
		Title:   "Invalid PubSub subscription config",
		Summary: "Subscriptions names on topics must be unique.",
		Detail: combine(
			resourceNameHelpKebabCase("pubsub subscription", "name"),
			pubsubHelp,
		),
		Locations: SrcLocations{first, second},
	}, false)
}

func PubSubSubscriptionConfigNotConstant(fileset *token.FileSet, fieldName string, node ast.Node) error {
	return errinsrc.New(ErrParams{
		Code:    30,
		Title:   "Invalid PubSub subscription config",
		Summary: fmt.Sprintf("All values in pubsub.SubscriptionConfig must be a constant, however %s was not a constant.", fieldName),
		Detail: combine(
			pubsubNewSubscriptionHelp,
			pubsubHelp,
		),
		Locations: SrcLocations{FromGoASTNodeWithTypeAndText(fileset, node, LocError, "got "+nodeType(node))},
	}, false)
}

func PubSubSubscriptionRequiresHandler(fileset *token.FileSet, cfgNode ast.Node) error {
	return errinsrc.New(ErrParams{
		Code:    31,
		Title:   "Invalid PubSub subscription config",
		Summary: "pubsub.NewSubscription requires the configuration field named \"Handler\" to populated with the subscription handler function.",
		Detail: combine(
			pubsubNewSubscriptionHelp,
			pubsubHelp,
		),
		Locations: SrcLocations{FromGoASTNode(fileset, cfgNode)},
	}, false)
}

func PubSubSubscriptionHandlerNotInService(fileset *token.FileSet, funcRef ast.Node, funcDecl ast.Node) error {
	locations := SrcLocations{FromGoASTNode(fileset, funcDecl)}

	if funcRef != nil {
		locations[0].Text = "defined here"
		locations = append(locations, FromGoASTNodeWithTypeAndText(fileset, funcRef, LocHelp, "passed to the config here"))
	}

	return errinsrc.New(ErrParams{
		Code:    32,
		Title:   "Invalid PubSub subscription config",
		Summary: "The function passed to `pubsub.NewSubscription` must be declared in the the same service as the subscription.",
		Detail: combine(
			pubsubNewSubscriptionHelp,
			pubsubHelp,
		),
		Locations: locations,
	}, false)
}

func PubSubSubscriptionInvalidField(fileset *token.FileSet, fieldName string, requirement string, node ast.Node, was string) error {
	return errinsrc.New(ErrParams{
		Code:    33,
		Title:   "Invalid PubSub subscription config",
		Summary: fmt.Sprintf("%s must be %s.", fieldName, requirement),
		Detail: combine(
			pubsubNewSubscriptionHelp,
			pubsubHelp,
		),
		Locations: SrcLocations{FromGoASTNodeWithTypeAndText(fileset, node, LocError, "was "+was)},
	}, false)
}

func PubSubAttrInvalidTag(fileset *token.FileSet, node ast.Node, fieldName string) error {
	return errinsrc.New(ErrParams{
		Code:    34,
		Title:   "Invalid PubSub message attribute",
		Summary: "PubSub message attributes must not be prefixed with \"encore\".",
		Detail: combine(
			pubsubHelp,
		),
		Locations: SrcLocations{FromGoASTNodeWithTypeAndText(fileset, node, LocError, fmt.Sprintf("try \"%s\"", fieldName[6:]))},
	}, false)
}

func PubSubPublishInvalidLocation(fileset *token.FileSet, node ast.Node) error {
	return errinsrc.New(ErrParams{
		Code:    35,
		Title:   "Invalid PubSub publish",
		Summary: "PubSub publish calls must be made in the either from within a service or from within a global middleware function",
		Detail: combine(
			pubsubHelp,
		),
		Locations: SrcLocations{FromGoASTNode(fileset, node)},
	}, false)
}

func ResourceNameNotSnakeCase(fileset *token.FileSet, node ast.Node, resourceType string, paramName string, name string) error {
	proposedName := idents.GenerateSuggestion(name, idents.SnakeCase)

	return errinsrc.New(ErrParams{
		Code:      36,
		Title:     "Invalid resource name",
		Summary:   fmt.Sprintf("The %s must be %s be defined in \"snake_case\"", resourceType, paramName),
		Detail:    resourceNameHelpKebabCase(resourceType, paramName),
		Locations: SrcLocations{FromGoASTNodeWithTypeAndText(fileset, node, LocError, fmt.Sprintf("try %q?", proposedName))},
	}, false)
}

func ResourceNameReserved(fileset *token.FileSet, node ast.Node, resourceType string, paramName string, name, reservedPrefix string, isSnakeCase bool) error {
	suggestion := ""
	if strings.HasPrefix(name, reservedPrefix) { // should always be the case, but better to be safe
		suggestion = fmt.Sprintf("try %q?", name[len(reservedPrefix):])
	}

	var detail string
	if isSnakeCase {
		detail = resourceNameHelpSnakeCase(resourceType, paramName)
	} else {
		detail = resourceNameHelpKebabCase(resourceType, paramName)
	}

	return errinsrc.New(ErrParams{
		Code:  37,
		Title: "Reserved resource name",
		// The metrics.NewCounter metric name "e_blah" uses the reserved prefix "e_".
		Summary:   fmt.Sprintf("The %s %s %q uses the reserved prefix %q", resourceType, paramName, name, reservedPrefix),
		Detail:    detail,
		Locations: SrcLocations{FromGoASTNodeWithTypeAndText(fileset, node, LocError, suggestion)},
	}, false)
}

func MetricLabelsNotNamedStruct(fileset *token.FileSet, node ast.Node, resourceType string, schema *schema.Type) error {
	return errinsrc.New(ErrParams{
		Code:  38,
		Title: "Invalid metric labels type",
		// The metrics.NewCounterGroup labels type must be a named struct type
		Summary: fmt.Sprintf("The %s labels type must be a named struct type", resourceType),
		Detail: combine(
			metricsHelp,
		),
		Locations: SrcLocations{FromGoASTNodeWithTypeAndText(fileset, node, LocError, fmt.Sprintf("was given %s", schemaType(schema)))},
	}, false)
}

func MetricLabelReservedName(fileset *token.FileSet, node ast.Node, fieldName, label string) error {
	return errinsrc.New(ErrParams{
		Code:    39,
		Title:   "Reserved metrics label",
		Summary: fmt.Sprintf("The %s field uses the reserved metrics label %s", fieldName, label),
		Detail: combine(
			metricsHelp,
		),
		Locations: SrcLocations{FromGoASTNode(fileset, node)},
	}, false)
}

func MetricLabelsFieldInvalidType(fileset *token.FileSet, node ast.Node, resourceType, fieldName string, fieldType *schema.Type) error {
	return errinsrc.New(ErrParams{
		Code:  40,
		Title: "Invalid metric labels type",
		// The metrics.NewCounterGroup labels type must be a named struct type
		Summary: fmt.Sprintf("The %s labels type's field named %s must be a string, boolean, or integer", resourceType, fieldName),
		Detail: combine(
			metricsHelp,
		),
		Locations: SrcLocations{FromGoASTNodeWithTypeAndText(fileset, node, LocError, fmt.Sprintf("was given %s", schemaType(fieldType)))},
	}, false)
}

func MetricLabelsIsPointer(fileset *token.FileSet, node ast.Node, resourceType string) error {
	return errinsrc.New(ErrParams{
		Code:  41,
		Title: "Invalid metric labels type",
		// The metrics.NewCounterGroup labels type must be a named struct type
		Summary: fmt.Sprintf("The %s labels type must be a non-pointer named struct, got a pointer type", resourceType),
		Detail: combine(
			metricsHelp,
		),
		Locations: SrcLocations{FromGoASTNode(fileset, node)},
	}, false)
}

func MetricReferencedInOtherService(fileset *token.FileSet, reference ast.Node, defined ast.Node) error {
	refLoc := FromGoASTNode(fileset, reference)
	refLoc.Text = "referenced here"

	definedLoc := FromGoASTNode(fileset, defined)
	definedLoc.Type = LocHelp
	definedLoc.Text = "defined here"

	return errinsrc.New(ErrParams{
		Code:      42,
		Title:     "Cross service metric reference",
		Summary:   "A metric defined within a service can only be referenced from within that same service.",
		Detail:    metricsHelp,
		Locations: SrcLocations{refLoc, definedLoc},
	}, false)
}

func NoServicesFound() error {
	return errinsrc.New(ErrParams{
		Code:    43,
		Title:   "No services found",
		Summary: "No services were found in the application.",
		Detail:  serviceHelp,
	}, false)
}

func ServiceContainedWithinAnotherService(outerName string, innerName string) error {
	return errinsrc.New(ErrParams{
		Code:  44,
		Title: "Service contained within another service",
		Summary: fmt.Sprintf(
			"The service %s was found within the service %s. Encore does not allow services to be nested",
			innerName, outerName,
		),
		Detail: serviceHelp,
	}, false)
}

func ServicesWithSameName(name string, firstLocation string, secondLocation string) error {
	return errinsrc.New(ErrParams{
		Code:  45,
		Title: "Duplicate service name",
		Summary: fmt.Sprintf(
			"Two services were found with the same name \"%s\", services must have unique names. The services where found:\n\t%s\n\t%s",
			name,
			firstLocation,
			secondLocation,
		),
		Detail: serviceHelp,
	}, false)
}

func MultipleServiceStructsFound(fileset *token.FileSet, serviceName string, initialNode ast.Node, secondNode ast.Node) *errinsrc.ErrInSrc {
	return errinsrc.New(ErrParams{
		Code:  46,
		Title: "Multiple service structs found",
		Summary: fmt.Sprintf(
			"Multiple service structs were found in service \"%s\". Encore only allows one service struct to be defined per service.",
			serviceName,
		),
		Detail: serviceHelp,
		Locations: SrcLocations{
			FromGoASTNodeWithTypeAndText(fileset, initialNode, LocError, "first service struct found here"),
			FromGoASTNodeWithTypeAndText(fileset, secondNode, LocError, "second service struct found here"),
		},
	}, false)
}

func MultipleAuthHandlersFound(fileset *token.FileSet, initialNode ast.Node, secondNode ast.Node) *errinsrc.ErrInSrc {
	return errinsrc.New(ErrParams{
		Code:  47,
		Title: "Multiple auth handlers found",
		Summary: fmt.Sprintf(
			"Multiple auth handlers were found in the application. Encore only allows one auth handler to be defined per application.",
		),
		Detail: authHelp,
		Locations: SrcLocations{
			FromGoASTNodeWithTypeAndText(fileset, initialNode, LocError, "first auth handler found here"),
			FromGoASTNodeWithTypeAndText(fileset, secondNode, LocError, "second auth handler found here"),
		},
	}, false)
}
