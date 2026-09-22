# Read-only Rhino 8 prerequisite check. The plugin may return only OK.
import Rhino

doc = Rhino.RhinoDoc.ActiveDoc
assert doc is not None, 'Open a Rhino document first'
assert doc.ModelUnitSystem == Rhino.UnitSystem.Millimeters, 'Expected millimetres'
assert 0 < doc.ModelAbsoluteTolerance <= 0.05, 'Expected tolerance <= 0.05 mm'
for obj in doc.Objects.GetSelectedObjects(False, False):
    assert obj.Geometry.IsValid, 'Selected object has invalid geometry'
    box = obj.Geometry.GetBoundingBox(True)
    print('Selected: {} / {}; size: {:.3f}, {:.3f}, {:.3f} mm'.format(
        obj.Attributes.Name or '(unnamed)', obj.Geometry.ObjectType,
        box.Max.X-box.Min.X, box.Max.Y-box.Min.Y, box.Max.Z-box.Min.Z))
